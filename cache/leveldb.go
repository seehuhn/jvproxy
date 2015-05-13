package cache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/seehuhn/jvproxy/cache/pb"
	"github.com/seehuhn/trace"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	"golang.org/x/crypto/sha3"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	indexDirName = "index"
	metaDirName  = "meta"
	newDirName   = "new"
)

const hashLen = 32

type sample struct {
	hash    []byte
	useTime int64
	size    int64
}

type ldbCache struct {
	baseDir string
	newDir  string
	index   *leveldb.DB
	meta    *leveldb.DB

	stats  *pb.Stats // TODO(voss): needed?
	submit chan *sample
}

// NewLevelDBCache creates a new `Cache` object, with on-disk backing
// store in the directory `baseDir`.  If an existing cache is
// discovered in `baseDir`, this cache is used, otherwise a new cache
// is created.
func NewLevelDBCache(baseDir string) (Cache, error) {
	// create store directory hierarchy
	directories := []string{
		baseDir,
	}
	for i := 0; i < 256; i++ {
		part := fmt.Sprintf("%02x", i)
		directories = append(directories, filepath.Join(baseDir, part))
	}
	indexDir := filepath.Join(baseDir, indexDirName)
	directories = append(directories, indexDir)
	metaDir := filepath.Join(baseDir, metaDirName)
	directories = append(directories, metaDir)
	newDir := filepath.Join(baseDir, newDirName)
	directories = append(directories, newDir)

	didCreate := false
	for _, dirName := range directories {
		err := os.Mkdir(dirName, 0755)
		if err != nil && !os.IsExist(err) {
			return nil, err
		}
		didCreate = didCreate || (err == nil)
	}
	if didCreate {
		trace.T("jvproxy/cache", trace.PrioInfo,
			"created store directories under %s", baseDir)
	}

	index, err := leveldb.OpenFile(indexDir, nil)
	if err != nil {
		return nil, err
	}

	meta, err := leveldb.OpenFile(metaDir, nil)
	if err != nil {
		return nil, err
	}

	res := &ldbCache{
		baseDir: baseDir,
		newDir:  newDir,
		index:   index,
		meta:    meta,
		submit:  make(chan *sample, 16),
	}
	res.loadStats()
	go res.manageIndex()

	return res, nil
}

func (cache *ldbCache) Close() error {
	return cache.meta.Close()
}

func (cache *ldbCache) Retrieve(req *http.Request) []*Entry {
	res := make([]*Entry, 0, 1)

	url := req.URL.String()
	keyPfx := make([]byte, len(url)+1)
	copy(keyPfx, url)
	limits := util.BytesPrefix(keyPfx)
	iter := cache.meta.NewIterator(limits, nil)
	defer func() {
		iter.Release()
		err := iter.Error()
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while using levelDB iterator: %s", err.Error())
		}
	}()
	for iter.Next() {
		key := iter.Key()
		_, fields, values := keyToURL(key)
		if !varyHeadersMatch(fields, values, req.Header) {
			continue
		}

		hashes := iter.Value()
		metaHash := hashes[:hashLen]
		contentHash := hashes[hashLen:]

		metaData := cache.loadLdbMetaData(metaHash)
		if metaData == nil {
			continue
		}

		entry := &Entry{
			MetaData: *metaData,
			GetBody: func() io.ReadCloser {
				fname := cache.getStoreName(contentHash)
				body, err := os.Open(fname)
				if err == nil {
					fi, err := body.Stat()
					if err != nil {
						trace.T("jvproxy/cache", trace.PrioError,
							"cannot stat %s: %s", fname, err.Error())
						return nil
					}
					cache.submit <- &sample{
						hash:    contentHash,
						useTime: time.Now().Unix(),
						size:    fi.Size(),
					}
				} else {
					if !os.IsNotExist(err) {
						trace.T("jvproxy/cache", trace.PrioError,
							"cannot read %s: %s", fname, err.Error())
					}
				}
				return body
			},
			CacheID: contentHash,
			Source:  "cache",
		}

		res = append(res, entry)
	}
	return res
}

func (cache *ldbCache) StoreStart(url string, meta *MetaData) StoreCont {
	metaHash := cache.storeLdbMetaData(meta)
	if metaHash == nil {
		return nil
	}

	store, err := ioutil.TempFile(cache.newDir, "")
	if err != nil {
		panic(err)
	}
	return &ldbEntry{
		cache:    cache,
		store:    store,
		hash:     sha3.NewShake128(),
		metaHash: metaHash,
		key:      urlToKey(url, meta.Header),
	}
}

func (cache *ldbCache) Update(url string, entry *Entry) {
	metaHash := cache.storeLdbMetaData(&entry.MetaData)
	if metaHash == nil {
		return
	}

	key := urlToKey(url, entry.Header)
	hash := make([]byte, 2*hashLen)
	copy(hash[:hashLen], metaHash)
	copy(hash[hashLen:], entry.CacheID)
	cache.meta.Put(key, hash, nil)
}

func (cache *ldbCache) loadLdbMetaData(hash []byte) *MetaData {
	file, err := os.Open(cache.getStoreName(hash))
	if err != nil {
		return nil
	}
	defer file.Close()

	res := &MetaData{}
	dec := gob.NewDecoder(file)
	err = dec.Decode(res)
	if err != nil {
		return nil
	}

	return res
}

func (cache *ldbCache) storeLdbMetaData(m *MetaData) []byte {
	buf := bytes.Buffer{}
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(m)
	if err != nil {
		panic(err)
	}
	data := buf.Bytes()

	store, err := ioutil.TempFile(cache.newDir, "")
	if err != nil {
		panic(err)
	}
	tmpName := store.Name()
	defer os.Remove(tmpName)
	_, e1 := store.Write(data)
	e2 := store.Close()
	if e1 != nil || e2 != nil {
		return nil
	}

	hash := make([]byte, hashLen)
	sha3.ShakeSum128(hash, data)
	storeName := cache.getStoreName(hash)
	os.Link(tmpName, storeName)
	return hash
}

func (cache *ldbCache) getStoreName(hash []byte) string {
	a := fmt.Sprintf("%02x", hash[0])
	b := fmt.Sprintf("%x", hash[1:])
	return filepath.Join(cache.baseDir, a, b)
}

func urlToKey(url string, header http.Header) []byte {
	// TODO(voss): check that url contains no '\0' bytes?  To be safe,
	// maybe encode strings with leanding length (using the
	// encoding/binary module)?
	res := []byte{}
	res = append(res, []byte(url)...)
	res = append(res, 0)

	varyFields := getVaryFields(header)
	n := len(varyFields)
	res = append(res, byte(n/256), byte(n%256))
	varyValues := getNormalizedHeaders(varyFields, header)
	for i, name := range varyFields {
		res = append(res, name...)
		res = append(res, 0)
		res = append(res, varyValues[i]...)
		if i < n-1 {
			res = append(res, 0)
		}
	}
	return res
}

func keyToURL(key []byte) (url string, fields []string, values []string) {
	pos := bytes.IndexByte(key, '\000')
	url, key = string(key[:pos]), key[pos+1:]
	n := int(key[0])*256 + int(key[1])
	key = key[2:]
	for i := 0; i < n; i++ {
		pos = bytes.IndexByte(key, '\000')
		var name string
		name, key = string(key[:pos]), key[pos+1:]
		fields = append(fields, name)
		var value []byte
		if i < n-1 {
			pos = bytes.IndexByte(key, '\000')
			value, key = key[:pos], key[pos+1:]
		} else {
			value = key
		}
		values = append(values, string(value))
	}
	return
}

type ldbEntry struct {
	cache    *ldbCache
	store    *os.File
	hash     sha3.ShakeHash
	metaHash []byte
	key      []byte
}

func (entry *ldbEntry) Reader(r io.Reader) io.Reader {
	return io.TeeReader(io.TeeReader(r, entry.hash), entry.store)
}

func (entry *ldbEntry) Commit(size int64) {
	tmpName := entry.store.Name()
	defer func() {
		err := os.Remove(tmpName)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"cannot remove temporary file %q: %s", tmpName, err.Error())
		}
	}()

	err := entry.store.Close()
	if err != nil {
		return
	}

	hash := make([]byte, 2*hashLen)
	copy(hash[:hashLen], entry.metaHash)
	contentHash := hash[hashLen:]
	_, err = io.ReadFull(entry.hash, contentHash)
	if err != nil {
		panic(err)
	}
	storeName := entry.cache.getStoreName(contentHash)

	err = os.Link(tmpName, storeName)
	if err == nil {
		trace.T("jvproxy/cache", trace.PrioDebug,
			"new cache entry %s", storeName)
	} else {
		if !os.IsExist(err) {
			trace.T("jvproxy/cache", trace.PrioError,
				"cannot create %s: %s", storeName, err.Error())
		}
		return
	}

	err = entry.cache.meta.Put(entry.key, hash, nil)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"cannot store cache entry in leveldb: %s", err.Error())
		return
	}

	entry.cache.submit <- &sample{
		hash:    contentHash,
		useTime: time.Now().Unix(),
		size:    size,
	}
}

func (entry *ldbEntry) Discard() {
	tmpName := entry.store.Name()
	entry.store.Close()
	err := os.Remove(tmpName)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"cannot remove temporary file %q: %s", tmpName, err.Error())
	}
}
