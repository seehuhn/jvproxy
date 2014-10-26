package main

import (
	"bytes"
	"code.google.com/p/go.crypto/sha3"
	"encoding/gob"
	"fmt"
	"github.com/seehuhn/trace"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
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
	Retrieve(*http.Request) *proxyResponse
	StoreStart(url string, statusCode int, header http.Header) CacheEntry
	Close() error
}

type CacheEntry interface {
	Reader(io.Reader) io.Reader
	Complete()
	Abort()
}

type NullCache struct{}

func (cache *NullCache) Retrieve(*http.Request) *proxyResponse {
	return nil
}

func (cache *NullCache) StoreStart(string, int, http.Header) CacheEntry {
	return &nullEntry{}
}

func (cache *NullCache) Close() error {
	return nil
}

type nullEntry struct{}

func (entry *nullEntry) Reader(r io.Reader) io.Reader {
	return r
}
func (entry *nullEntry) Complete() {}
func (entry *nullEntry) Abort()    {}

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

func (cache *ldbCache) getStoreName(hash []byte) string {
	a := fmt.Sprintf("%02x", hash[0])
	b := fmt.Sprintf("%x", hash[1:])
	return filepath.Join(cache.baseDir, a, b)
}

type ldbMetaData struct {
	StatusCode int
	Header     http.Header
}

func (cache *ldbCache) loadLdbMetaData(hash []byte) *ldbMetaData {
	// TODO(voss): unpack directly into a proxyResponse?
	res := &ldbMetaData{}

	file, err := os.Open(cache.getStoreName(hash))
	if err != nil {
		return nil
	}
	defer file.Close()

	dec := gob.NewDecoder(file)
	err = dec.Decode(res)
	if err != nil {
		return nil
	}

	return res
}

func (cache *ldbCache) storeLdbMetaData(m *ldbMetaData) []byte {
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

func urlToKey(url string, header http.Header) []byte {
	// TODO(voss): check that url contains no '\0' bytes?
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

func (cache *ldbCache) Retrieve(req *http.Request) *proxyResponse {
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
		// TODO(voss): check freshness/lifetime

		hashes := iter.Value()
		metaHash := hashes[:hashLen]
		metaData := cache.loadLdbMetaData(metaHash)
		if metaData == nil {
			continue
		}
		contentHash := hashes[hashLen:]
		body, err := os.Open(cache.getStoreName(contentHash))
		if err != nil {
			continue
		}
		return &proxyResponse{
			StatusCode: metaData.StatusCode,
			Header:     metaData.Header,
			Body:       body,
		}
	}
	return nil
}

func (cache *ldbCache) StoreStart(url string, status int, header http.Header) CacheEntry {
	m := &ldbMetaData{
		StatusCode: status,
		Header:     header,
	}
	metaHash := cache.storeLdbMetaData(m)
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

func (cache *ldbCache) Close() error {
	return cache.DB.Close()
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

func (entry *ldbEntry) Complete() {
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

	entry.cache.DB.Put(entry.key, hash, nil)
}

func (entry *ldbEntry) Abort() {
	tmpName := entry.store.Name()
	entry.store.Close()
	err := os.Remove(tmpName)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"cannot remove temporary file %q: %s", tmpName, err.Error())
	}
}
