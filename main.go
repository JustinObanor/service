package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
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

var (
	db       *sql.DB
	host     = getenv("PSQL_HOST", "objects")
	port     = getenv("PSQL_PORT", "5432")
	user     = getenv("PSQL_USER", "postgres")
	password = getenv("PSQL_PWDcas", "12345")
	dbname   = getenv("PSQL_DB_NAME", "objects")
)

func newObjectRequest() *ObjectList {
	return &ObjectList{
		ObjectIDs: make([]int, 0, 200),
	}
}

func main() {
	var (
		err                error
		httpAddr           = flag.String("http", ":8080", "http listen address")
		callbackHTTPAddr   = flag.String("api1", ":9090", "http listen address for callbacks body")
		objectInfoHTTPAddr = flag.String("api2", ":9010", "http listen address for getting info by object id")
	)

	db, err = sql.Open("postgres", fmt.Sprintf(`host=%s port=%s user=%s
		password=%s dbname=%s sslmode=disable`,
		host, port, user, password, dbname))
	if err != nil {
		log.Fatal("error opening connection to postgres: ", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal("error pinging postgres client: ", err)
	}

	flag.Parse()

	errs := make(chan error)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		errs <- fmt.Errorf("%s", <-sig)
	}()

	objectIds := newObjectRequest()

	callbackHandler := mux.NewRouter()
	callbackHandler.HandleFunc("/callback", objectIds.callback()).Methods("GET")
	go func() {
		log.Printf("listening on port %s for object ids\n", *callbackHTTPAddr)
		http.ListenAndServe(*callbackHTTPAddr, callbackHandler)
	}()

	objectInfoHandler := mux.NewRouter()
	objectInfoHandler.HandleFunc("/", objectIds.fetchDetail()).Methods("GET")
	go func() {
		log.Printf("listening on port %s for object info by id\n", *objectInfoHTTPAddr)
		http.ListenAndServe(*objectInfoHTTPAddr, objectInfoHandler)
	}()

	go func() {
		log.Println("listening on port", *httpAddr)
		log.Fatal(http.ListenAndServe(*httpAddr, nil))
	}()

	log.Println("exit", <-errs)
	log.Println("finished")
}

//objectIDs receives the IDs from localhost:9090/callback
func (objectIDs *ObjectList) callback() func(_ http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		ticker := time.NewTicker(time.Second * 5)
		for range ticker.C {
			if r.Body == nil {
				continue
			}

			if err := json.NewDecoder(r.Body).Decode(&objectIDs); err != nil {
				log.Println("error retrieving object ids")
				http.Error(w, "error retrieving object ids", http.StatusBadRequest)
				return
			}
			fmt.Fprintln(w, "received object ids")
		}
	}
}

type result struct {
	index int
	item  ObjectDetail
	err   error
}

// fetchDetail calls localhost:9010/objects/:id to receive details of an object by its id
func (objectIDs ObjectList) fetchDetail() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		resChan := make(chan result)

		for idx, id := range objectIDs.ObjectIDs {
			path := fmt.Sprintf("objects/:%d", id)

			go func(url string, index int) {
				resp, err := http.Get(url)
				if err != nil {
					resChan <- result{index: index, err: err}
				}
				defer resp.Body.Close()

				obj := ObjectDetail{}
				if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
					resChan <- result{err: err}
				}

				obj.LastSeen = time.Now()

				resChan <- result{index: index, item: obj}
			}(path, idx)
		}

		objectsInfo := make([]ObjectDetail, len(objectIDs.ObjectIDs))
		for res := range resChan {
			if res.err != nil {
				continue
			}
			objectsInfo[res.index] = res.item

		}
		
		filter(objectsInfo)
		store(objectsInfo)
	}
}

// store stores object details to psql
func store(objectsInfo []ObjectDetail) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			log.Println("db instance is nil")
			http.Error(w, "db instance is nil", http.StatusInternalServerError)
			return
		}

		for _, info := range objectsInfo {
			sql := fmt.Sprintf("into into %s (id, online, lastseen) values($1, $2, $3)", dbname)

			if _, err := db.ExecContext(r.Context(), sql, info.ID, info.Online, info.LastSeen); err != nil {
				log.Println("error inserting object details")
				http.Error(w, "error inserting object details", http.StatusInternalServerError)
				continue
			}
		}
		log.Println("inserted object details")
		fmt.Fprintln(w, "inserted object details")
	}
}

// filter filters the list of objects by online status and stores in a hash table
func filter(objectsInfo []ObjectDetail) map[bool]int {
	res := make(map[bool]int)
	for _, info := range objectsInfo {
		res[info.Online] = info.ID
	}

	return res
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
