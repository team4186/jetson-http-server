package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"os"

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

func nvpads(w, h, fps int) string {
	return fmt.Sprintf(" ! video/x-raw(memory:NVMM),format=(string)I420,width=(int)%d,height=(int)%d,framerate=(fraction)%d/1 ", w, h, fps)
}
func pads(w, h, fps int) string {
	return fmt.Sprintf("  ! video/x-raw,format=(string)I420,width=(int)%d,height=(int)%d,framerate=(fraction)%d/1 ", w, h, fps)
}
func nvcamera() string { return " nvcamerasrc fpsRange=\"60.0 60.0\"" }
func camera(cameraid int, name string) string {
	path := fmt.Sprintf("/dev/video%d", cameraid)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Sprintf(" videotestsrc pattern=0 ")
	} else {
		return fmt.Sprintf(" v4l2src device=\"%s\" name=%s ", path, name)
	}
}
func to_mix_sink(mix string, sink int) string { return fmt.Sprintf(" ! queue ! %s.sink_%d", mix, sink) }
func udp_sink(client, port string) string {
	return fmt.Sprintf(" ! udpsink clients=%s:%s ", client, port)
}
func videoscaler(width int, height int) string { return fmt.Sprintf(" ! videoscale ! video/x-raw,width=(int)%d,height=(int)%d ", width, height) }
func rotate(method int) string { return fmt.Sprintf(" ! videoflip method=%d ", method) }
func nvflip() string           { return " ! nvvidconv flip-method=vertical-flip " }
func vp8_rtp_pack() string {
	return "  ! omxvp8enc name=encoder control-rate=3 bitrate=400000 ! rtpvp8pay "
}
func vp9_rtp_pack() string {
	return "  ! vp9enc name=encoder control-rate=3 bitrate=400000 ! rtpvp9pay "
}
func h265_rtp_pack() string {
	return "  ! omxh265enc name=encoder control-rate=3 bitrate=400000 ! rtph265pay "
}
func video_mixer(name string) string { return fmt.Sprintf("videomixer name=%s background=1 ", name) }
func mixer_sink(id, x, y int) string {
	return fmt.Sprintf(" sink_%d::xpos=%d sink_%d::ypos=%d ", id, x, id, y)
}

func create_pipeline(client, port, layout, hertz string) (*gst.Pipeline, error) {
	parsedInt, err := strconv.ParseInt(hertz, 10, 64)
	if err != nil {
		return nil, err
	}
	hertzInt := int(parsedInt)

	var pipeline *gst.Pipeline
	switch {
	case strings.HasPrefix(layout, "only"):
		pipeline, err = create_pipeline_single(client, port, layout, hertzInt)

	default:
		pipeline, err = create_pipeline_default(client, port, hertzInt)
	}

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

	return pipeline, nil

}

func create_pipeline_default(client, port string, hertz int) (*gst.Pipeline, error) {


	config:=video_mixer("mix") +
			mixer_sink(0, 0, 0) +
			mixer_sink(1, 240, 0) +
			mixer_sink(2, 640, 240) +
			mixer_sink(3, 450, 330) +

			vp8_rtp_pack() +
			udp_sink(client, port) +

			camera(1, "pseye0") + pads(640, 480, hertz) + to_mix_sink("mix", 0) +
			camera(0, "pseye1") + pads(640, 480, hertz) + videoscaler(160,120) + to_mix_sink("mix", 1) 
			//camera(2, "pseye2") + pads(640, 480, hertz) + videoscaler(320, 240) + to_mix_sink("mix", 2) +
			//camera(3, "pseye3") + pads(640, 480, hertz) + videoscaler(160, 120) + to_mix_sink("mix", 3)

	log.Println(config)

	return gst.ParseLaunch(config)
}

func create_pipeline_single(client, port, layout string, hertz int) (*gst.Pipeline, error) {
	cameraIndex, err := strconv.ParseInt(layout[len(layout)-1:], 10, 64)
	if err != nil {
		return nil, err
	}

	config :=	video_mixer("mix") +
			mixer_sink(0, 0, 0) +

			vp8_rtp_pack() +
			udp_sink(client, port) +

			camera(int(cameraIndex), "pseye0") + pads(640,480, hertz) + to_mix_sink("mix", 0)

	log.Println(config)

	return gst.ParseLaunch(config)
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

func camera_routine(client, port, layout, hertz string, loopchan chan *glib.MainLoop) {
	pipeline, err := create_pipeline(client, port, layout, hertz)
	if err != nil {
		FEEDBACK <- err.Error()
		loopchan <- nil
	} else {
		html := `udpsrc port=%s caps="application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)VP9,payload=(int)96" ! rtpvp9depay ! vp9dec ! autovideosink`

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

func start_cam(client, port, layout, hertz string) {
	log.Printf("Start Cam @%s to=%s:%s layout=%s", hertz, client, port, layout)
	loopchan := make(chan *glib.MainLoop)

	go camera_routine(client, port, layout, hertz, loopchan)

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

			hertz := r.URL.Query().Get("hertz")
			if hertz == "" {
				hertz = "30"
			}

			layout := r.URL.Query().Get("layout")

			go stop_cam(client)
			<-CAM_STOPED
			go start_cam(client, port, layout, hertz)
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

func handler_ping(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "pong")
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
	http.HandleFunc("/ping", handler_ping)
	http.ListenAndServe(":5800" , nil)
	log.Println("Server Shutdown")
}
