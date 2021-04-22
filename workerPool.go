package main

import (
	"context"
	"log"
	"time"
)

//worker blocks until the object ids are available, and sends gotten details to result
func (req *client) worker(ctx context.Context, jobs <-chan []int, seen map[int]struct{}, result chan<- ObjectDetail) {
	for {
		for ids := range jobs {
			for _, objectID := range ids {
				req.RLock()
				_, ok := seen[objectID]
				req.RUnlock()
				if ok {
					continue
				}

				req.Lock()
				seen[objectID] = struct{}{}
				req.Unlock()

				go func(id int) {
					detail, err := req.fetchDetail(ctx, id)
					if err != nil {
						log.Println(err)
						return
					}

					detail.LastSeen = time.Now().UTC()
					result <- detail
				}(objectID)
			}
		}
	}
}

//filter pulls the details sent to the result channel by worker()
func (db *database) filter(ctx context.Context, result <-chan ObjectDetail) {
	for {
		for detail := range result {
			if detail.Online {
				if err := db.toStore(ctx, detail); err != nil {
					log.Println(err)
					continue
				}
			}
		}
	}
}
