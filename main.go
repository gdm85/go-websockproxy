package main

import (
	"io"
	"fmt"
	"net/http"
	"io/ioutil"

	"golang.org/x/net/websocket"
)

type proxy struct {
}

func (p proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
    if err != nil {
        panic(err)
    }
	fmt.Printf("received %d bytes: [", len(body))
    for _, b := range body {
        fmt.Printf("%x ", b)
    }
    fmt.Println("]")
}

func echoHandler(ws *websocket.Conn) {
	io.Copy(ws, ws)
}

func main() {
//	http.Handle("/echo", websocket.Handler(echoHandler))
	err := http.ListenAndServe(":1890", proxy{})
	if err != nil {
		panic("ListenAndServe: " + err.Error())
	}
}
