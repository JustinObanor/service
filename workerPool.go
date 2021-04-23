package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

//worker blocks until the object ids are available, and sends gotten details to result
func (c *client) worker(ctx context.Context, jobs <-chan []int, seen map[int]struct{}, result chan<- ObjectDetail) {
	for {
		for ids := range jobs {
			fmt.Println("got ids length: ", len(ids))
			for _, objectID := range ids {
				c.RLock()
				_, ok := seen[objectID]
				c.RUnlock()
				if ok {
					continue
				}
				fmt.Println(objectID)
				c.Lock()
				seen[objectID] = struct{}{}
				c.Unlock()

				go func(id int) {
					detail, err := c.fetchDetail(ctx, id)
					if err != nil {
						c.errChan <- err
						return
					}

					detail.LastSeen = time.Now().UTC()
					result <- detail
				}(objectID)
			}
		}
	}
}

func (c *client) errors() {
	for {
		for err := range c.errChan {
			log.Println(err)
		}
	}
}

//filter pulls the details sent to the result channel by worker()
func (db *database) filter(ctx context.Context, result <-chan ObjectDetail) {
	for {
		for detail := range result {
			if !detail.Online {
				continue
			}

			if err := db.toStore(ctx, detail); err != nil {
				db.errChan <- err
				continue
			}
			fmt.Printf("%+v\n", detail)
		}
	}
}

func (db *database) errors() {
	for {
		for err := range db.errChan {
			log.Println(err)
		}
	}
}
