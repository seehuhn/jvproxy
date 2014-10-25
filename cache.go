package main

import (
	"io"
	"io/ioutil"
	"net/http"

	"fmt"
	"github.com/seehuhn/trace"
	"github.com/syndtr/goleveldb/leveldb"
	"os"
	"path/filepath"
)

const (
	indexDirName = "index"
	newDirName   = "new"
)

type Cache interface {
	Retrieve(*http.Request) *proxyResponse
	StoreStart(statusCode int, headers http.Header) CacheEntry
	Close() error
}

type CacheEntry interface {
	Body() io.Writer
	Abort()
	Complete()
}

type NullCache struct{}

func (cache *NullCache) Retrieve(*http.Request) *proxyResponse {
	return nil
}

func (cache *NullCache) StoreStart(int, http.Header) CacheEntry {
	return &nullEntry{}
}

func (cache *NullCache) Close() error {
	return nil
}

type nullEntry struct{}

func (entry *nullEntry) Body() io.Writer {
	return ioutil.Discard
}
func (entry *nullEntry) Abort()    {}
func (entry *nullEntry) Complete() {}

type ldbCache struct {
	newDir string
	db     *leveldb.DB
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
		newDir: newDir,
		db:     db,
	}, nil
}

func (cache *ldbCache) Retrieve(*http.Request) *proxyResponse {
	return nil
}

func (cache *ldbCache) StoreStart(int, http.Header) CacheEntry {
	return &nullEntry{}
}

func (cache *ldbCache) Close() error {
	return cache.db.Close()
}
