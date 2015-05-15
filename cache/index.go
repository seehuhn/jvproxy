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
)

// Keys with len different from `hashLen` cannot occur for real cache
// entries.  We use keys of the form `[]byte{x}` to store metadata,
// where values of `x` are given in the following list.
const (
	statsKey byte = 1
)

const (
	bigFileLimit = 1024
	statsGroups  = 5
)

const (
	scanChunkSize  = 16
	pruneChunkSize = 1000
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

func (cache *ldbCache) loadStats() {
	var stats *pb.Stats
	key := []byte{statsKey}
	raw, err := cache.index.Get(key, nil)
	if err == nil {
		stats = &pb.Stats{}
		err := proto.Unmarshal(raw, stats)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding stats data: %s",
				err.Error())
			stats = nil
		} else if stats.GetVersion() != 0 {
			trace.T("jvproxy/cache", trace.PrioCritical,
				"unknown stats version %d",
				stats.GetVersion())
			panic("unknown stats version")
		}
	} else if err != leveldb.ErrNotFound {
		trace.T("jvproxy/cache", trace.PrioError,
			"error while reading stats data: %s",
			err.Error())
	}

	if stats == nil {
		stats = &pb.Stats{
			Hits: make([]uint32, 2*statsGroups),
			Sum:  make([]float64, 2*statsGroups),
		}
	}
	cache.stats = stats
	fmt.Println("loaded", cache.stats)
}

func (cache *ldbCache) saveStats() {
	raw, err := proto.Marshal(cache.stats)
	if err != nil {
		panic(err)
	}
	key := []byte{statsKey}
	err = cache.index.Put(key, raw, nil)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"error while writing stats data: %s",
			err.Error())
	}
}

func (cache *ldbCache) indexExistingEntries(res chan<- *sample) {
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
		count, ByteSize(totalSize))
}

func getGroup(data *pb.Entry) int {
	group := data.GetUseCount() - 1
	if group < 0 {
		group = 0
	} else if group >= statsGroups {
		group = statsGroups - 1
	}
	if data.GetSize() >= bigFileLimit {
		group += statsGroups
	}
	return int(group)
}

func (cache *ldbCache) pruneData() candidates {
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
		key := iter.Key()
		if len(key) == 1 {
			// special keys used to store meta-information in the DB
			continue
		}
		var score float64 = 0.0
		raw := iter.Value()
		data := &pb.Entry{}
		err := proto.Unmarshal(raw, data)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding index entry: %s",
				err.Error())
			score = math.MaxFloat64
		} else {
			score = -float64(data.GetLastUsed())
		}

		p = p.add(key, data.GetSize(), score)

		// group := getGroup(data)
		// lambda := float64(1.0)
		// if cache.stats.Hits[group] > 0 {
		//	lambda = float64(cache.stats.Hits[group]) / cache.stats.Sum[group]
		// } else {
		//	hits := uint32(0)
		//	for _, h := range cache.stats.Hits {
		//		hits += h
		//	}
		//	if hits > 0 {
		//		sum := float64(0.0)
		//		for _, s := range cache.stats.Sum {
		//			sum += s
		//		}
		//		lambda = float64(hits) / sum
		//	}
		// }
		// size := data.GetSize()
		// fmt.Printf("%x, %10d bytes, %2d hits, %12.5f bytes/second\n",
		//	key, size, data.GetUseCount(), float64(size)*lambda)
	}
	return p
}

func (cache *ldbCache) pruneMetadata() {
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
	fmt.Println("pruned", count, "entries")
}

// updateIndex updates statistical information in the index.  This
// method is *not* goroutine-safe.
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
		group := getGroup(data)
		n := data.GetUseCount()
		if n < math.MaxInt32 {
			n++
		}
		dt := time - data.GetLastUsed()
		data.LastUsed = proto.Int64(time)
		data.UseCount = proto.Int32(n)

		if dt > 0 {
			cache.stats.Hits[group]++
			cache.stats.Sum[group] += float64(dt)
			if cache.stats.Hits[group] > math.MaxInt32/2 {
				cache.stats.Hits[group] /= 2
				cache.stats.Sum[group] /= 2
			}
		}
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
	prune := make(chan candidates)

	go func() {
		cache.indexExistingEntries(primordial)

		p := cache.pruneData()
		prune <- p

		cache.pruneMetadata()
	}()

	var totalBytes int64
	var limit int64 = 64 * 1024 * 1024
	for {
		select {
		case entry, ok := <-cache.submit:
			if !ok {
				trace.T("jvproxy/cache", trace.PrioDebug,
					"stopping cache manager for %d", cache.baseDir)
				return
			}
			n := cache.updateIndex(entry.hash, entry.useTime, entry.size, false)
			totalBytes += n
			cache.saveStats() // TODO(voss): put in a time-delay
		case entry := <-primordial:
			n := cache.updateIndex(entry.hash, entry.useTime, entry.size, true)
			totalBytes += n
		case list := <-prune:
			for _, x := range list {
				if totalBytes < limit {
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
				// TODO(voss): update stats data
				totalBytes -= x.size
				fmt.Printf("remove %x (%d bytes) -> %d\n",
					x.hash, x.size, totalBytes)
			}
		}
	}
}
