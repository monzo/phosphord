package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	log "github.com/cihub/seelog"

	"github.com/mondough/phosphor/util"
	"github.com/mondough/phosphord/forwarder"
	"github.com/mondough/phosphord/transport"
)

const (
	UDP = "udp"
)

var (
	packetSize  = 65536 - 8 - 20 // 8-byte UDP header, 20-byte IP header
	bindAddress = "0.0.0.0:7760"

	// forwarder defaults
	numForwarders = 20
	bufferSize    = 200

	// logLevel default set to info and above
	defaultLogLevel = "info"
	verboseLogLevel = "debug"

	// verbose logging mode
	verbose = false

	// NSQ settings
	nsqdTCPAddrs = util.StringArray{}
	nsqTopic     = "trace"
)

func init() {
	// Register flags
	flag.BoolVar(&verbose, "verbose", false, "enable verbose logging")

	flag.IntVar(&numForwarders, "num-forwarders", 20, "set the number of workers which buffer and forward traces")
	flag.IntVar(&bufferSize, "buffer-size", 200, "set the maximum number of traces buffered per worker before batch sending")

	flag.Var(&nsqdTCPAddrs, "nsqd-tcp-address", "nsqd TCP address (may be given multiple times)")
	flag.StringVar(&nsqTopic, "nsq-topic", "trace", "nsq topic to forward traces to")
}

func main() {
	flag.Parse()

	// Set up the logger, using the log level set by the environment
	initialiseLogger()
	defer log.Flush()

	log.Infof("Phosphor started at %v using %v CPUs", time.Now(), runtime.NumCPU())

	// Use ALL the CPUs
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Make a channel to pass around trace frames
	ch := make(chan []byte)

	// Initialise our transport
	tr, err := transport.NewNSQTransport(nsqTopic, nsqdTCPAddrs)
	if err != nil {
		log.Criticalf(err.Error())
		os.Exit(1)
	}

	// Fire up a number of forwarders to process inbound messages
	forwarder.Verbose = verbose
	forwarder.Start(ch, tr, numForwarders, bufferSize)

	// Bind and listen to UDP traffic
	if err := listen(ch); err != nil {
		log.Criticalf(err.Error())
		os.Exit(1)
	}
}

// listen on a UDP socket for trace frames
func listen(ch chan []byte) error {

	// Resolve bind address
	address, err := net.ResolveUDPAddr(UDP, bindAddress)
	if err != nil {
		log.Errorf("Failed to resolve address: %s", err.Error())
		return err
	}

	// Take the resolved address and attempt to listen on the UDP socket
	listener, err := net.ListenUDP(UDP, address)
	if err != nil {
		log.Errorf("ListenUDP error: %s", err.Error())
		return err
	}
	defer listener.Close()

	// Listen loop
	log.Infof("Listening on %s for UDP trace frames", address.String())
	for {
		message := make([]byte, packetSize)
		n, _, err := listener.ReadFrom(message)
		if err != nil {
			continue
		}
		buf := bytes.NewBuffer(message[0:n])
		// log.Infof("Packet received from %s: %s", remaddr, string(message[0:n]))

		// Attempt to push into our channel to be processed by a worker
		select {
		case ch <- buf.Bytes():
			// log.Infof("Wrote message to channel")
		default:
			// abort!
			// log.Infof("Dropped message")
		}
	}
}

func initialiseLogger() {

	// Attempt to pull log level from env, if not set to default level
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = defaultLogLevel
	}

	// Overridden by verbose flag
	if verbose {
		logLevel = verboseLogLevel
	}

	// Build config for seelog
	logConfig := fmt.Sprintf(`<seelog minlevel="%s">`, logLevel)
	logConfig = strings.Join([]string{logConfig, `<outputs formatid="main"><console/></outputs><formats><format id="main" format="%Date %Time [%LEV] %Msg (%File %Line)%n"/></formats></seelog>`}, "")

	// Initialise the logger!
	logger, err := log.LoggerFromConfigAsBytes([]byte(logConfig))
	if err != nil {
		log.Errorf("Couldn't initialise new logger: %s", err)
	}
	log.ReplaceLogger(logger)
}
