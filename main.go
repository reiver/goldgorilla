package main

import (
	"flag"
	"strings"
)

func main() {
	svcAddr := flag.String("svc-addr", "http://localhost:8080", "service baseurl to register in logjam ( shouldn't end with / )")
	src := flag.String("src", ":8080", "listenHost:listenPort")
	logjamBaseUrl := flag.String("logjam-base-url", "http://localhost:8090", "logjam base url( shouldn't end with / )")
	targetRoom := flag.String("targetRoom", "test", "target room")
	icetcpmuxListenPort := flag.Uint("ice-tcp-mux-listen-port", 4444, "listen port to use for tcp ice candidates")
	flag.Parse()

	if strings.HasSuffix(*logjamBaseUrl, "/") {
		panic("logjam-base-url shouldn't end with /")
	}
	if strings.HasSuffix(*svcAddr, "/") {
		panic("service address shouldn't end with /")
	}
	app := App{}
	app.Init(*src, *svcAddr, *logjamBaseUrl, *targetRoom, *icetcpmuxListenPort)
	app.Run()
}
