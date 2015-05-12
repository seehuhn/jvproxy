package cache

import (
	"io"
	"net/http"
)

type NullCache struct{}

func (cache *NullCache) Retrieve(*http.Request) []*Entry {
	return nil
}

func (cache *NullCache) StoreStart(string, *MetaData) StoreCont {
	return &nullEntry{}
}

func (cache *NullCache) Update(url string, entry *Entry) {}

func (cache *NullCache) Close() error {
	return nil
}

type nullEntry struct{}

func (entry *nullEntry) Reader(r io.Reader) io.Reader {
	return r
}
func (entry *nullEntry) Commit(int64) {}
func (entry *nullEntry) Discard()     {}
