package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/ziutek/glib"
	"github.com/ziutek/gst"
)

var (
	CAM_START  = make(chan *http.Request)
	CAM_STOP   = make(chan string)
	CAM_STOPED = make(chan string)
	FEEDBACK   = make(chan string)
)

var running_clients = map[string](chan string){}

func create_pipeline(client string, port string) (*gst.Pipeline, error) {
	pipeline, err := gst.ParseLaunch(
		"videomixer name=mix background=1 " +
			" sink_1::ypos=240 " +
			" sink_2::ypos=480 " +
			" sink_3::xpos=320 sink_3::ypos=480 " +
			" sink_4::xpos=640 sink_4::ypos=480 " +
			" sink_5::xpos=320 " +
			"  ! omxvp8enc name=encoder control-rate=3 bitrate=400000 " +
			"  ! rtpvp8pay " +
			fmt.Sprintf("  ! udpsink clients=%s:%s ", client, port) +
			"v4l2src device=\"/dev/video1\" name=pseye0 " +
			"  ! video/x-raw,format=(string)I420,width=(int)320,height=(int)240,framerate=(fraction)60/1 " +
			"  ! queue ! mix.sink_0 " +
			"videotestsrc pattern=0 " +
			"  ! video/x-raw,format=(string)I420,width=(int)320,height=(int)240,framerate=(fraction)30/1 " +
			"  ! queue ! mix.sink_1 " +
			"videotestsrc pattern=0 " +
			"  ! video/x-raw,format=(string)I420,width=(int)320,height=(int)240,framerate=(fraction)30/1 " +
			"  ! queue ! mix.sink_2 " +
			"videotestsrc pattern=0 " +
			"  ! video/x-raw,format=(string)I420,width=(int)320,height=(int)240,framerate=(fraction)30/1 " +
			"  ! queue ! mix.sink_3 " +
			"videotestsrc pattern=0 " +
			"  ! video/x-raw,format=(string)I420,width=(int)320,height=(int)240,framerate=(fraction)30/1 " +
			"  ! queue ! mix.sink_4 " +
			"nvcamerasrc fpsRange=\"60.0 60.0\"" +
			"  ! video/x-raw(memory:NVMM),format=(string)I420,width=(int)640,height=(int)480,framerate=(fraction)60/1 " +
			"  ! nvvidconv flip-method=vertical-flip " +
			"  ! tee name =t " +
			"  t. ! queue ! mix.sink_5") // +
	//"  t. ! queue ! videoconvert ! video/x-raw,format=(string)GRAY8 " +
	//"     ! appsink sync=false name=appsink max-buffers=1 drop=true")

	if err != nil {
		return nil, err
	}

	bus := pipeline.GetBus()
	bus.AddSignalWatch()
	bus.Connect("message", func(bus *gst.Bus, msg *gst.Message) {
		switch msg.GetType() {
		case gst.MESSAGE_STREAM_STATUS:
			fmt.Printf("Element %s is changed state.\n", msg.GetSrc().GetName())
		case gst.MESSAGE_EOS:
			pipeline.SetState(gst.STATE_NULL)
		case gst.MESSAGE_ERROR:
			pipeline.SetState(gst.STATE_NULL)
			err, debug := msg.ParseError()
			fmt.Printf("Error: %s (debug: %s)\n", err, debug)
		}

	}, nil)

	//appsink := (*AppSink)(pipeline.GetByName("appsink"))
	//go app_sink_routine(appsink)

	return pipeline, nil
}

func stop_cam(client string) {
	log.Printf("Request killing process [client=%s]\n", client)
	if running_clients[client] != nil {
		log.Printf("Killing process [client=%s]\n", client)
		running_clients[client] <- client
		running_clients[client] = nil
	} else {
		CAM_STOPED <- client
	}
}

func camera_routine(client, port string, loopchan chan *glib.MainLoop) {
	pipeline, err := create_pipeline(client, port)
	if err != nil {
		FEEDBACK <- err.Error()
		loopchan <- nil
	} else {
		html := `<!doctype html>
	<html>
		<head>
			<meta charset='utf-8'>
			<title>Jetson Camera Status</title>
		</head>
		<body>
			<p>Use:</p>
			<textarea>gst-launch-1.0 udpsrc port=%s caps="application/x-rtp,media=(string)video,clock-rate=(int)90000, encoding-name=(string)VP8-DRAFT-IETF-01,payload=(int)96" ! rtpvp8depay ! vp8dec ! d3dvideosink </textarea>
		</body>
	</html>`

		FEEDBACK <- fmt.Sprintf(html, port)

		pipeline.SetState(gst.STATE_PLAYING)
		mainloop := glib.NewMainLoop(nil)

		log.Println("Main loop Sending")
		loopchan <- mainloop
		mainloop.Run()

		pipeline.SetState(gst.STATE_NULL)
		pipeline.Unref()
		CAM_STOPED <- client
		log.Println("Main loop Ended")
	}
}

func start_cam(client, port string) {
	log.Println("Start Cam")
	loopchan := make(chan *glib.MainLoop)

	go camera_routine(client, port, loopchan)

	mainloop := <-loopchan
	if loopchan == nil {
		return
	}
	stop_channel := make(chan string)
	running_clients[client] = stop_channel
	<-stop_channel
	fmt.Println("Quiting mainloop.")
	mainloop.Quit()
}

func cam_loop() {
	for {
		select {
		case r := <-CAM_START:
			client := r.URL.Query().Get("client")
			if client == "" {
				client = strings.Split(r.RemoteAddr, ":")[0]
			}

			port := r.URL.Query().Get("port")
			if port == "" {
				port = "554"
			}

			go stop_cam(client)
			<-CAM_STOPED
			go start_cam(client, port)
		case c := <-CAM_STOP:
			go stop_cam(c)
			<-CAM_STOPED
		}
	}
}

func handler_params(w http.ResponseWriter, r *http.Request) {
	r.URL.Query().Get("a")
	fmt.Fprintf(w, "Done[%s] %s!\n", r.URL.Path[1:], r.URL.Query().Get("a"))
}

func handler_camera(w http.ResponseWriter, r *http.Request) {
	CAM_START <- r
	result := <-FEEDBACK
	fmt.Fprintf(w, result)
}

func handler_help(w http.ResponseWriter, r *http.Request) {
	client := strings.Split(r.RemoteAddr, ":")[0]
	html := `<!doctype html>
	<html>
		<head>
			<meta charset='utf-8'>
			<title>Jetson Camera Status</title>
		</head>
		<body>
			<p>you are connecting from: %s</p>
			<p><a href="/camera">request camera</a></p>
			%s
		</body>
	</html>`
	client_list := ""
	for k, _ := range running_clients {
		line := fmt.Sprintf("<p>client='%s'</p><br>", k)
		client_list = fmt.Sprintf("%s%s", client_list, line)
	}
	fmt.Fprintf(w, html, client, client_list)
}

func main() {
	log.Println("Server Initialized")
	go cam_loop()
	http.HandleFunc("/", handler_help)
	http.HandleFunc("/camera", handler_camera)
	http.HandleFunc("/params", handler_params)
	http.ListenAndServe(":5800", nil)
	log.Println("Server Shutdown")
}
