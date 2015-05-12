package cache

import (
	"io"
	"net/http"
	"time"
)

// Cache is the abstract interface for a cache for HTTP responses.
type Cache interface {
	// Retrieve returns all cache entries available for a given HTTP request.
	Retrieve(*http.Request) []*Entry

	// StoreStart initiates the process of storing a new server
	// response in the cache.  The metadata of the response (url,
	// headers, and status code) is provided as arguments to the
	// StoreStart call, the response body must be delivered to the
	// returned StoreCont object.
	StoreStart(url string, meta *MetaData) StoreCont

	// Update exists the metadata of an existing cache entry.
	Update(url string, entry *Entry)

	// Close makes sure all persistent data is stored on disk and
	// frees all resources associated with the cache.  The cache
	// cannot be used anymore after Close has been called.
	Close() error
}

// MetaData describes the metadata of a HTTP response for use in a
// caching proxy.
type MetaData struct {
	StatusCode    int
	Header        http.Header
	ResponseTime  time.Time
	ResponseDelay time.Duration
}

// Entry describes a stored HTTP response for use in a caching proxy.
type Entry struct {
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
	Commit(size int64)

	// Discard is used to signal that transfer of the server response
	// has not been received successfully (i.e. because the connection
	// was interrupted), and that the data written so far should be
	// discarded.
	Discard()
}
