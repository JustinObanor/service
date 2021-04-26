package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

//worker blocks until the object ids are available, and sends gotten details to result
func (c *client) worker(ctx context.Context, jobs <-chan int, result chan<- ObjectDetail) {
	for objectID := range jobs {
		c.RLock()
		_, ok := c.seen[objectID]
		c.RUnlock()
		if ok {
			continue
		}

		c.Lock()
		c.seen[objectID] = struct{}{}
		c.Unlock()

		detail, err := c.fetchDetail(ctx, objectID)
		if err != nil {
			c.errChan <- err
			continue
		}

		detail.LastSeen = time.Now().UTC()
		result <- detail
	}
}

func (c *client) errors() {
	for err := range c.errChan {
		log.Println(err)
	}
}

//filter pulls the details sent to the result channel by worker()
func (db *database) filter(ctx context.Context, result <-chan ObjectDetail) {
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

func (db *database) errors() {
	for err := range db.errChan {
		log.Println(err)
	}
}
