package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

// ObjectList holds the list of object ids
type ObjectList struct {
	ObjectIDs []int `json:"object_ids"`
}

// ObjectDetail holds the status of a single ID stored in postgres
type ObjectDetail struct {
	ID       int  `json:"id"`
	Online   bool `json:"online"`
	LastSeen time.Time
}

type database struct {
	db *sql.DB
}

type client struct {
	cli     *http.Client
	workers int
	path    string

	sync.RWMutex
}

var (
	host     = getenv("PSQL_HOST", "objects")
	port     = getenv("PSQL_PORT", "5432")
	user     = getenv("PSQL_USER", "postgres")
	password = getenv("PSQL_PWDcas", "12345")
	dbname   = getenv("PSQL_DB_NAME", "objects")
)

//new list of object ids
func newObjectList() *ObjectList {
	return &ObjectList{
		ObjectIDs: make([]int, 0, 200),
	}
}

//new http client for posting to path
func newHTTPClient() *client {
	return &client{
		cli: &http.Client{
			Timeout: time.Second * 5,
		},
		workers: 5,
		path:    "http://host.docker.internal:9010/objects/",
	}
}

//new psql database connection
func newDatabase() (*database, error) {
	db, err := sql.Open("postgres", fmt.Sprintf(`host=%s port=%s user=%s
		password=%s dbname=%s sslmode=disable`,
		host, port, user, password, dbname))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	log.Printf("connected to psql client, host: %s\n", host)

	return &database{
		db: db,
	}, nil
}

func main() {
	db, err := newDatabase()
	if err != nil {
		log.Fatal("error connecting to psql", err)
	}

	objList := newObjectList()
	req := newHTTPClient()

	callbackAddr := flag.String("callback", ":9090", "http listen address for callbacks body")
	flag.Parse()

	errChan := make(chan error)

	//handle shutdown signals
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		errChan <- fmt.Errorf("%s", <-sig)
	}()

	jobs := make(chan []int)
	seen := make(map[int]struct{})
	result := make(chan ObjectDetail)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go req.worker(ctx, jobs, seen, result)

	//receive object ids from /callback path
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			log.Println("nil request body")
			http.Error(w, "no request body found", http.StatusBadRequest)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&objList); err != nil {
			log.Println("error decoding request")
			http.Error(w, "error decoding request", http.StatusBadRequest)
			return
		}
		jobs <- objList.ObjectIDs
	})

	for i := 0; i < req.workers; i++ {
		go db.filter(ctx, result)
	}

	go func() {
		for {
			for range time.NewTicker(time.Second * 5).C {
				err := db.deleteDetail(ctx)
				if err != nil {
					return
				}
			}
		}
	}()

	//listening for callback
	go func() {
		log.Printf("listening on port %s for callback\n", *callbackAddr)
		if err := http.ListenAndServe(*callbackAddr, nil); err != nil {
			errChan <- err
			cancel()
		}
	}()

	log.Println("exit: ", <-errChan)
}
