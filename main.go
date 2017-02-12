package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

var running_cmds = map[string]*exec.Cmd{}
var (
	CAM_START = make(chan *http.Request)
	CAM_STOP  = make(chan string)
	FEEDBACK  = make(chan string)
)

func stop_cam(client string) {
	if running_cmds[client] != nil {
		log.Println("Killing process\n")
		running_cmds[client].Process.Kill()
		running_cmds[client] = nil
	}
}

func start_cam(client string, port string) {
	cmd := exec.Command("sh", "/home/ubuntu/cam-test.sh", client, port)
	err := cmd.Start()

	if err != nil {
		cmd = nil
		FEEDBACK <- err.Error()
	} else {
		html := `<!doctype html>
	<html>
		<head>
			<meta charset='utf-8'>
			<title>Jetson Camera Status</title>
		</head>
		<body>
			<p>Camera Initialized [pid=%d]!</p>
			<p>Use:</p>
			<p>gst-launch-1.0 udpsrc port=%s caps="application/x-rtp,media=(string)video,clock-rate=(int)90000, encoding-name=(string)VP8-DRAFT-IETF-01,payload=(int)96" ! rtpvp8depay ! vp8dec ! d3dvideosink </p>
		</body>
	</html>`

		running_cmds[client] = cmd
		FEEDBACK <- fmt.Sprintf(html, cmd.Process.Pid, port)
		cmd.Wait()
	}
}

func cam_loop() {
	for {
		select {
		case r := <-CAM_START:
			client := strings.Split(r.RemoteAddr, ":")[0]
			port := r.URL.Query().Get("port")
			if port == "" {
				port = "554"
			}
			stop_cam(client)
			go start_cam(client, port)
		case c := <-CAM_STOP:
			stop_cam(c)
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
	for k, v := range running_cmds {
		line := fmt.Sprintf("<p>client='%s' pid=%d</p><br>", k, v.Process.Pid)
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
