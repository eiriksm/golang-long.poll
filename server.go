package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	redis "github.com/garyburd/redigo/redis"
)

// Variables for redis connections.
var messages map[int]chan string = make(map[int]chan string)
var pool *redis.Pool
var redisServer = flag.String("redisServer", ":6379", "")

func newPool(server string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", server)
			if err != nil {
				return nil, err
			}
			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}


func IndexResponse(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "index.html")
}

func PollResponse(w http.ResponseWriter, req *http.Request) {
	id, _ := strconv.Atoi(req.URL.Query().Get("id"))
	log.Print("New long poll connection from " + strconv.Itoa(id))

	ch := messages[id]

	// new client?
	if ch == nil {
		ch = make(chan string)
		messages[id] = ch
	}

	// Goroutine for redis subscription.
	go func() {
		conn := pool.Get()
		defer conn.Close()
		psc := redis.PubSubConn{Conn: conn}
		psc.PSubscribe("__key*__:" + strconv.Itoa(id))
		for {
			switch n := psc.Receive().(type) {
			case redis.PMessage:
				log.Print("Received new message for " + strconv.Itoa(id))
				messages[id] <- string(n.Data)
				return;
			case redis.Subscription:
				if n.Count == 0 {
					return
				}
			case error:
				fmt.Printf("error: %v\n", n)
				return
			}
		}

	}()

	var timeout = 60000
	select {
	case msg := <-messages[id]:
		io.WriteString(w, msg)
		return
	case <-time.After(time.Duration(timeout) * time.Millisecond):
		log.Print("Timing out connection from " + strconv.Itoa(id))
		w.WriteHeader(204)
		return
	}
}

func valueSetter(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get("id")
	val := req.URL.Query().Get("value")
	log.Print("Setting a value for id " + id)
	conn := pool.Get()
	defer conn.Close()
	conn.Send("SET", id, val)
	conn.Flush()
	io.WriteString(w, "Thanks!")
}

func main() {
	pool = newPool(*redisServer)
	http.HandleFunc("/poll", PollResponse)
	http.HandleFunc("/set", valueSetter)
	http.Handle("/bower_components/", http.StripPrefix("/bower_components/", http.FileServer(http.Dir("bower_components/"))))
	http.HandleFunc("/", IndexResponse)
	log.Print("Starting server on :12345")
	err := http.ListenAndServe(":12345", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}

}
