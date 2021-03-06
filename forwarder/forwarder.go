package forwarder

import (
	"encoding/json"
	"time"

	log "github.com/cihub/seelog"
	"github.com/mattheath/goprotobuf/proto"

	pb "github.com/mondough/phosphor/proto"
	"github.com/mondough/phosphord/transport"
)

var Verbose bool

func Start(traces chan []byte, tr transport.Transport, numWorkers, bufferSize int) {

	log.Infof("Starting %v forwarders with buffer size of %v", numWorkers, bufferSize)

	for i := 0; i < numWorkers; i++ {
		f := &forwarder{
			id:          i,
			ch:          traces,
			tr:          tr,
			traceBuffer: make([][]byte, 0, bufferSize),
			bufferSize:  bufferSize,
		}

		// Do something useful
		go f.work()
	}

}

type forwarder struct {
	id          int
	ch          chan []byte
	tr          transport.Transport
	traceBuffer [][]byte
	bufferSize  int
}

func (f *forwarder) work() {

	log.Debugf("[Forwarder %v] started", f.id)

	var b []byte
	var i int
	var decoded *pb.Annotation
	var js []byte

	metricsTick := time.NewTicker(5 * time.Second)
	timeoutTick := time.NewTicker(2 * time.Second)

	for {
		select {
		case b = <-f.ch:
			i++

			// Log the frame if we're in verbose mode
			if Verbose {
				decoded = &pb.Annotation{}
				if err := proto.Unmarshal(b, decoded); err != nil {
					log.Warnf("[Forwarder %v] Couldn't decode trace frame", f.id)
					continue
				}
				js, _ = json.Marshal(decoded)
				log.Tracef("[Forwarder %v] Received message: %s", f.id, string(js))
			}

			// Add message to our buffer
			f.traceBuffer = append(f.traceBuffer, b)

			// Forward on if we're at our buffer size
			if len(f.traceBuffer) >= f.bufferSize {
				f.send()
			}
		case <-timeoutTick.C:
			f.send()
		case <-metricsTick.C:
			log.Debugf("[Forwarder %v] Processed %v messages", f.id, i)
		}
	}
}

func (f *forwarder) send() error {

	// Don't publish empty buffers
	if len(f.traceBuffer) == 0 {
		return nil
	}

	// Attempt to publish
	log.Debugf("[Forwarder %v] Sending %v traces", f.id, len(f.traceBuffer))
	if err := f.tr.MultiPublish(f.traceBuffer); err != nil {
		// we return an error here, but currently ignore it
		// therefore the behaviour will be reattempting to republish the
		// buffer when the next trace arrives to this forwarder
		return err
	}

	// Empty the buffer on success
	f.traceBuffer = nil

	return nil
}
