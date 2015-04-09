package jvproxy

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/seehuhn/trace"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	"golang.org/x/crypto/sha3"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
)

const (
	indexDirName = "index"
	newDirName   = "new"
)

const hashLen = 32

type Cache interface {
	// Retrieve returns all cache entries available for a given HTTP request.
	Retrieve(*http.Request) []*CacheEntry

	// StoreStart initiates the process of storing a new server
	// response in the cache.  The metadata of the response (url,
	// headers, and status code) is provided as arguments to the
	// StoreStart call, the response body must be delivered to the
	// returned StoreCont object.
	StoreStart(url string, statusCode int, header http.Header) StoreCont

	// Update exists the metadata of an existing cache entry.
	Update(url string, entry *CacheEntry)

	// Close makes sure all persistent data is stored on disk and
	// frees all resources associated with the cache.  The cache
	// cannot be used anymore after Close has been called.
	Close() error
}

type MetaData struct {
	StatusCode int
	Header     http.Header
}

type CacheEntry struct {
	MetaData
	GetBody func() io.ReadCloser
	CacheID []byte
	Source  string
}

// StoreCont objects are used to store a response body in the cache,
// after the metadata already has been stored in the cache using the
// Cache.StoreStart() method.
type StoreCont interface {
	// Reader returns an io.Reader which stores in the cache what it
	// reads from r.  The argument should normally be the .Body field
	// of the server response.  The resulting cache entry is stored in
	// temporary storage until either .Commit() or .Discard() is
	// called.
	Reader(r io.Reader) io.Reader

	// Commit is used to signal that the server response was received
	// successfully and that the response body should be committed to
	// persistent storage.
	Commit()

	// Discard is used to signal that transfer of the server response
	// has not been received successfully (i.e. because the connection
	// was interrupted), and that the data written so far should be
	// discarded.
	Discard()
}

type ldbCache struct {
	baseDir string
	newDir  string
	DB      *leveldb.DB
}

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
		trace.T("jvproxy/store", trace.PrioInfo,
			"created store directories under %s", baseDir)
	}

	db, err := leveldb.OpenFile(indexDir, nil)
	if err != nil {
		return nil, err
	}

	return &ldbCache{
		baseDir: baseDir,
		newDir:  newDir,
		DB:      db,
	}, nil
}

func (cache *ldbCache) Close() error {
	return cache.DB.Close()
}

func (cache *ldbCache) Retrieve(req *http.Request) []*CacheEntry {
	res := make([]*CacheEntry, 0, 1)

	url := req.URL.String()
	keyPfx := make([]byte, len(url)+1)
	copy(keyPfx, url)
	limits := util.BytesPrefix(keyPfx)
	iter := cache.DB.NewIterator(limits, nil)
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
		_, fields, values := keyToUrl(key)
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

		entry := &CacheEntry{
			MetaData: *metaData,
			GetBody: func() io.ReadCloser {
				body, _ := os.Open(cache.getStoreName(contentHash))
				return body
			},
			CacheID: contentHash,
			Source:  "cache",
		}

		res = append(res, entry)
	}
	return res
}

func (cache *ldbCache) StoreStart(url string, status int, header http.Header) StoreCont {
	meta := &MetaData{
		StatusCode: status,
		Header:     header,
	}
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
		key:      urlToKey(url, header),
	}
}

func (cache *ldbCache) Update(url string, entry *CacheEntry) {
	metaHash := cache.storeLdbMetaData(&entry.MetaData)
	if metaHash == nil {
		return
	}

	key := urlToKey(url, entry.Header)
	hash := make([]byte, 2*hashLen)
	copy(hash[:hashLen], metaHash)
	copy(hash[hashLen:], entry.CacheID)
	cache.DB.Put(key, hash, nil)
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

func keyToUrl(key []byte) (url string, fields []string, values []string) {
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

func (entry *ldbEntry) Commit() {
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
	}
	// TODO(voss): check for errors different from an existing cache
	// entry

	err = entry.cache.DB.Put(entry.key, hash, nil)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"cannot store cache entry in leveldb: %s", err.Error())
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
