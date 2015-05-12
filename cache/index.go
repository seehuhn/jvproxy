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
)

const chunkSize = 16

func (cache *ldbCache) indexExistingEntries(res chan<- *stats) {
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
			files, err := f.Readdir(chunkSize)
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
				res <- &stats{
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
		"found %d pre-existing cache entries, %d bytes total",
		count, totalSize)
}

func (cache *ldbCache) secateurs() {
	iter := cache.index.NewIterator(nil, nil)
	for iter.Next() {
		key := iter.Key()
		raw := iter.Value()
		data := &pb.Entry{}
		err := proto.Unmarshal(raw, data)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding index entry: %s",
				err.Error())
		}
		fmt.Printf("%x %v\n", key, data)
	}
}

func (cache *ldbCache) updateIndex(hash []byte, time, size int64, new bool) {
	var data *pb.Entry
	raw, err := cache.index.Get(hash, nil)
	if err == nil {
		data = &pb.Entry{}
		err = proto.Unmarshal(raw, data)
		if err != nil {
			trace.T("jvproxy/cache", trace.PrioError,
				"error while decoding index entry: %s",
				err.Error())
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
		return
	}

	if data == nil {
		data = &pb.Entry{
			LastUsed: proto.Int64(time),
			Size:     proto.Int64(size),
			UseCount: proto.Int32(1),
		}
	} else {
		n := data.GetUseCount()
		if n < math.MaxInt32 {
			n++
		}
		data.LastUsed = proto.Int64(time)
		data.UseCount = proto.Int32(n)
	}

	raw, err = proto.Marshal(data)
	err = cache.index.Put(hash, raw, nil)
	if err != nil {
		trace.T("jvproxy/cache", trace.PrioError,
			"error while writing index entry: %s",
			err.Error())
	}
}

func (cache *ldbCache) manageIndex() {
	primordial := make(chan *stats, chunkSize)

	go func() {
		cache.indexExistingEntries(primordial)

		cache.secateurs()
	}()

	for {
		select {
		case entry, ok := <-cache.stats:
			if !ok {
				trace.T("jvproxy/cache", trace.PrioDebug,
					"stopping cache manager for %d", cache.baseDir)
				return
			}
			cache.updateIndex(entry.hash, entry.useTime, entry.size, false)
		case entry := <-primordial:
			cache.updateIndex(entry.hash, entry.useTime, entry.size, true)
		}
	}
}
