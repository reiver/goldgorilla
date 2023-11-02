package main

import (
	"flag"
	"strings"
)

func main() {
	src := flag.String("src", ":8080", "listenHost:listenPort")
	logjamBaseUrl := flag.String("logjam-base-url", "http://localhost:8090", "logjam base url( shouldn't end with / )")
	icetcpmuxListenPort := flag.Uint("ice-tcp-mux-listen-port", 4444, "listen port to use for tcp ice candidates")
	customICEHostCandidateIP := flag.String("custom-ice-host-candidate-ip", "", "set to override host ice candidates address")
	flag.Parse()

	if strings.HasSuffix(*logjamBaseUrl, "/") {
		panic("logjam-base-url shouldn't end with /")
	}
	app := App{}
	*logjamBaseUrl += "/goldgorilla"
	app.Init(*src, *logjamBaseUrl, *icetcpmuxListenPort, *customICEHostCandidateIP)
	app.Run()
}
