package cache

import (
	"encoding/hex"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/seehuhn/jvproxy/cache/pb"
	"github.com/seehuhn/trace"
	"github.com/syndtr/goleveldb/leveldb"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	scanChunkSize  = 16
	pruneChunkSize = 1000
	lowWaterMark   = 48 * 1024 * 1024
	highWaterMark  = 49 * 1024 * 1024
)

type victim struct {
	hash  []byte
	size  int64
	score float64
}
type candidates []victim

func (p candidates) add(hash []byte, size int64, score float64) candidates {
	hash = append([]byte{}, hash...)
	n := len(p)
	if n >= pruneChunkSize && p[n-1].score >= score {
		return p
	}
	idx := sort.Search(n, func(i int) bool {
		return p[i].score < score
	})

	if idx == n {
		if n >= pruneChunkSize {
			return p
		}
		return append(p, victim{hash, size, score})
	}

	if n < pruneChunkSize {
		p = append(p, victim{})
		copy(p[idx+1:n+1], p[idx:n])
	} else {
		copy(p[idx+1:n], p[idx:n-1])
	}
	p[idx] = victim{hash, size, score}
	return p
}

func (cache *ldbCache) indexExistingData(res chan<- *sample) {
	trace.T("jvproxy/cache", trace.PrioDebug,
		"starting to index cache dir %s",
		cache.baseDir)
	var count int64
	var totalSize int64
	for i := 0; i < 256; i++ {
		part := fmt.Sprintf("%02x", i)
		dir := filepath.Join(cache.baseDir, part)

		f, err := os.Open(dir)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"cannot open cache directory %s: %s",
				dir, err.Error())
			goto next
		}
		for {
			files, err := f.Readdir(scanChunkSize)
			if err != nil {
				if err != io.EOF {
					trace.T("jvproxy/cache", trace.PrioError,
						"cannot read cache directory %s: %s",
						dir, err.Error())
				}
				break
			}
			for _, fi := range files {
				size := fi.Size()
				name := part + fi.Name()
				hash, err := hex.DecodeString(name)
				if err != nil {
					trace.T("jvproxy/cache", trace.PrioError,
						"malformed cache entry name %s/%s: %s",
						part, fi.Name(), err.Error())
					continue
				}
				res <- &sample{
					hash:    hash,
					useTime: fi.ModTime().Unix(),
					size:    size,
				}
				count++
				totalSize += size
			}
		}
	next:
		f.Close()
	}
	trace.T("jvproxy/cache", trace.PrioInfo,
		"found %d pre-existing cache entries, %s total",
		count, byteSize(totalSize))
}

func (cache *ldbCache) pruneData() candidates {
	trace.T("jvproxy/cache", trace.PrioDebug,
		"starting to prune data")

	p := candidates{}

	iter := cache.index.NewIterator(nil, nil)
	defer func() {
		iter.Release()
		err := iter.Error()
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while using levelDB iterator: %s", err.Error())
		}
	}()
	for iter.Next() {
		var score float64 // higher score = evicted sooner
		raw := iter.Value()
		data := &pb.Entry{}
		err := proto.Unmarshal(raw, data)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding index entry: %s",
				err.Error())
			score = math.MaxFloat64 // always evict invalid metadata
		} else {
			score = -float64(data.GetLastUsed())
		}

		p = p.add(iter.Key(), data.GetSize(), score)
	}

	trace.T("jvproxy/cache", trace.PrioDebug,
		"identified %d data for pruning", len(p))
	return p
}

func (cache *ldbCache) pruneMetadata() {
	trace.T("jvproxy/cache", trace.PrioDebug,
		"starting to prune metadata")

	iter := cache.meta.NewIterator(nil, nil)
	defer func() {
		iter.Release()
		err := iter.Error()
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while using levelDB iterator: %s", err.Error())
		}
	}()
	count := 0
	for iter.Next() {
		key := iter.Key()
		hashes := iter.Value()
		contentHash := hashes[hashLen:]
		present, err := cache.index.Has(contentHash, nil)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while checking for key presence: %s", err.Error())
		} else if !present {
			err = cache.meta.Delete(key, nil)
			if err != nil {
				trace.T("jvproxy/cache", trace.PrioError,
					"error while deleting DB entry: %s", err.Error())
			} else {
				count++
			}
		}
	}
	trace.T("jvproxy/cache", trace.PrioInfo,
		"pruned %d metadata entries", count)
}

// updateIndex updates the information about the data with the given
// hash in the index.  This method is *not* goroutine-safe.
func (cache *ldbCache) updateIndex(hash []byte, time, size int64, new bool) int64 {
	var data *pb.Entry
	raw, err := cache.index.Get(hash, nil)
	if err == nil {
		data = &pb.Entry{}
		err = proto.Unmarshal(raw, data)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding index entry: %s",
				err.Error())
			data = nil
		}
	} else if err != leveldb.ErrNotFound {
		trace.T("jvproxy/cache", trace.PrioError,
			"error while reading index entry: %s",
			err.Error())
	}

	if data != nil && data.GetSize() != size {
		trace.T("jvproxy/cache", trace.PrioError,
			"index entry with wrong size: db=%d, file=%d",
			data.GetSize(), size)
		data = nil
	}

	if new && data != nil {
		return data.GetSize()
	}

	var res int64
	if data == nil {
		data = &pb.Entry{
			LastUsed: proto.Int64(time),
			Size:     proto.Int64(size),
			UseCount: proto.Int32(1),
		}
		res = size
	} else {
		data.LastUsed = proto.Int64(time)
		n := data.GetUseCount()
		if n < math.MaxInt32 {
			n++
		}
		data.UseCount = proto.Int32(n)
	}

	raw, err = proto.Marshal(data)
	if err != nil {
		panic(err)
	}
	err = cache.index.Put(hash, raw, nil)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"error while writing index entry: %s",
			err.Error())
	}

	return res
}

func (cache *ldbCache) manageIndex() {
	primordial := make(chan *sample, scanChunkSize)
	type pruneRequest struct {
		c    candidates
		wait chan<- struct{}
	}
	prune := make(chan *pruneRequest)

	var totalBytes int64
	var pruneMutex sync.Mutex
	pruneCond := sync.NewCond(&pruneMutex)

	go func() {
		cache.indexExistingData(primordial)

		for {
			// TODO(voss): implement a method to abort this loop

			// wait until high watermark is reached
			pruneMutex.Lock()
			for totalBytes <= highWaterMark {
				pruneCond.Wait()
			}
			pruneMutex.Unlock()

			candidates := cache.pruneData()
			wait := make(chan struct{})
			prune <- &pruneRequest{
				c:    candidates,
				wait: wait,
			}
			_ = <-wait
			cache.pruneMetadata()
		}
	}()

	for {
		select {
		case entry, ok := <-cache.submit:
			if !ok {
				trace.T("jvproxy/cache", trace.PrioDebug,
					"stopping cache manager for %d", cache.baseDir)
				return
			}
			n := cache.updateIndex(entry.hash, entry.useTime, entry.size, false)
			pruneMutex.Lock()
			totalBytes += n
			if totalBytes > highWaterMark {
				pruneCond.Signal()
			}
			pruneMutex.Unlock()
		case entry := <-primordial:
			n := cache.updateIndex(entry.hash, entry.useTime, entry.size, true)
			pruneMutex.Lock()
			totalBytes += n
			if totalBytes > highWaterMark {
				pruneCond.Signal()
			}
			pruneMutex.Unlock()
		case req := <-prune:
			count := 0
			var prunedSize int64
			pruneMutex.Lock()
			for _, x := range req.c {
				if totalBytes <= lowWaterMark {
					break
				}
				fname := cache.getStoreName(x.hash)
				err := os.Remove(fname)
				if err != nil {
					trace.T("jvproxy/cache", trace.PrioError,
						"cannot remove %s: %s", fname, err.Error())
				}
				err = cache.index.Delete(x.hash, nil)
				if err != nil {
					trace.T("jvproxy/cache", trace.PrioError,
						"cannot delete index entry %x: %s", x.hash, err.Error())
				}
				count++
				prunedSize += x.size
				totalBytes -= x.size
			}
			trace.T("jvproxy/cache", trace.PrioInfo,
				"pruned %d data (%s total), cache is now %s",
				count, byteSize(prunedSize), byteSize(totalBytes))
			pruneMutex.Unlock()
			close(req.wait)
		}
	}
}
