package main

import (
	"flag"
	"strings"
)

func main() {
	svcAddr := flag.String("svc-addr", "", "service baseurl to register in logjam ( shoudln't end with / )")
	src := flag.String("src", ":8080", "listenhost:listenPort")
	logjamBaseUrl := flag.String("logjam-base-url", "https://example.com", "logjam base url(shouldn't end with /)")
	targetRoom := flag.String("targetRoom", "testyroom", "target room")

	flag.Parse()

	if strings.HasSuffix(*logjamBaseUrl, "/") {
		panic("logjam-base-url shouldn't end with /")
	}
	if strings.HasSuffix(*svcAddr, "/") {
		panic("service address shouldn't end with /")
	}
	app := App{}
	app.Init(*src, *svcAddr, *logjamBaseUrl, *targetRoom)
	app.Run()
}
