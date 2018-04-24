package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	pb "istio-stackdriver/helloworld"
	"log"
	http "net/http"
	"time"

	openzipkin "github.com/openzipkin/zipkin-go"
	zrh "github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/trace"
	// 	"go.opencensus.io/exporter/stackdriver"
	//	"go.opencensus.io/trace"
)

var (
	port = flag.String("port", "8080", "http port")
)

func extractHeaders(r *http.Request) map[string]string {
	headers := []string{
		"x-request-id",
		"x-b3-traceid",
		"x-b3-spanid",
		"x-b3-parentspanid",
		"x-b3-sampled",
		"x-b3-flags",
		"x-ot-span-context",
	}

	ret := map[string]string{}
	for _, key := range headers {
		val := r.Header.Get(key)
		if val != "" {
			ret[key] = val
		}
	}
	return ret
}

func buildTraceID(s string) ([16]byte, error) {
	tid := [16]byte{}

	l := hex.DecodedLen(len(s))
	decoded, err := hex.DecodeString(s)

	if err != nil {
		return tid, err
	}
	for i := 0; i < 16; i++ {
		if i < l {
			tid[i] = 0
		} else {
			tid[i] = decoded[i-l]
		}
	}
	return tid, err
}

func buildSpanID(s string) ([8]byte, error) {
	sid := [8]byte{}

	l := hex.DecodedLen(len(s))
	decoded, err := hex.DecodeString(s)

	if err != nil {
		return sid, err
	}
	for i := 0; i < 8; i++ {
		if i < l {
			sid[i] = 0
		} else {
			sid[i] = decoded[i-l]
		}
	}
	return sid, nil
}

func execWorkflow(headers map[string]string) {
	tid, err := buildTraceID(headers["x-b3-traceid"])
	if err != nil {
		fmt.Println(err)
		return
	}
	sid, err := buildSpanID(headers["x-b3-spanid"])
	if err != nil {
		fmt.Println(err)
		return
	}

	p := trace.SpanContext{
		TraceID: tid,
		SpanID:  sid,
	}
	ctx, span := trace.StartSpanWithRemoteParent(context.Background(), "workflow", p)
	span.spanContext.SpanID = sid
	_, span1 := trace.StartSpan(ctx, "foo")
	time.Sleep(50 * time.Millisecond)
	span1.End()
	_, span2 := trace.StartSpan(ctx, "bar")
	time.Sleep(50 * time.Millisecond)
	span2.End()
	span.End()

}

func svcBGreeting(req *http.Request) (string, error) {
	conn, err := grpc.Dial("svc-b:50051", grpc.WithInsecure())
	if err != nil {
		return "", fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)
	eh := extractHeaders(req)
	log.Printf("tracing header: %v", eh)

	// Contact the server and print out its response.
	name := "svcA"

	ctx := context.Background()
	for key, val := range eh {
		ctx = metadata.AppendToOutgoingContext(ctx, key, val)
	}

	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("could not greet: %v", err)
	}

	execWorkflow(eh)
	return r.Message, nil
}

func EchoHandler(writer http.ResponseWriter, request *http.Request) {
	writer.Write([]byte("Hello this is svcA\n"))
	if m, err := svcBGreeting(request); err == nil {
		writer.Write([]byte("Greeting from svcB: " + m + "\n"))
	} else {
		log.Printf("%v", err)
	}
}

func main() {
	flag.Parse()
	http.HandleFunc("/", EchoHandler)

	// Initialize open census zipkin exporter
	endpoint, err := openzipkin.NewEndpoint("svc-a-workload", "")
	if err != nil {
		log.Println(err)
	}

	// The Zipkin reporter takes collected spans from the app and reports them to the backend
	// http://localhost:9411/api/v2/spans is the default for the Zipkin Span v2
	reporter := zrh.NewReporter("http://zipkin.istio-system:9411/api/v2/spans")
	defer reporter.Close()

	// The OpenCensus exporter wraps the Zipkin reporter
	exporter := zipkin.NewExporter(reporter, endpoint)
	trace.RegisterExporter(exporter)

	// For example purposes, sample every trace.
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	//	exporter, err := stackdriver.NewExporter(stackdriver.Options{
	//		ProjectID:            "csm-metrics-test",
	//		BundleDelayThreshold: time.Second / 10,
	//		BundleCountThreshold: 10})
	//	if err != nil {
	//		log.Println(err)
	//	}
	//	trace.RegisterExporter(exporter)
	//	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	http.ListenAndServe(":"+*port, nil)
}
