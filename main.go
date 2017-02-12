package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

type cmd_data struct {
	gst_pid      string
	stop_channel chan string
}

var running_cmds = map[string]*cmd_data{}

var (
	CAM_START = make(chan *http.Request)
	CAM_STOP  = make(chan string)
	FEEDBACK  = make(chan string)
)

func stop_cam(client string) {
	if running_cmds[client] != nil {
		log.Printf("Killing process [pid=%s]\n", running_cmds[client].gst_pid)
		running_cmds[client].stop_channel <- client
		running_cmds[client] = nil
	}
}

func start_cam(client string, port string) {
	cmd := exec.Command("sh", "/home/ubuntu/cam-test.sh", client, port)
	out, err := cmd.CombinedOutput()

	if err != nil {
		FEEDBACK <- err.Error()
	} else {
		html := `<!doctype html>
	<html>
		<head>
			<meta charset='utf-8'>
			<title>Jetson Camera Status</title>
		</head>
		<body>
			<p>Camera Initialized [pid=%s]!</p>
			<p>Use:</p>
			<textarea>gst-launch-1.0 udpsrc port=%s caps="application/x-rtp,media=(string)video,clock-rate=(int)90000, encoding-name=(string)VP8-DRAFT-IETF-01,payload=(int)96" ! rtpvp8depay ! vp8dec ! d3dvideosink </textarea>
		</body>
	</html>`

		pid := string(out[:len(out)-1])
		log.Printf("Running at [pid=%s]\n", pid)
		data := cmd_data{pid, make(chan string)}
		running_cmds[client] = &data
		FEEDBACK <- fmt.Sprintf(html, pid, port)
		<-data.stop_channel
		killcmd := exec.Command("kill", pid)
		killcmd.Start()
		killcmd.Wait()
		log.Printf("[pid=%s] finished!\n", pid)
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
		line := fmt.Sprintf("<p>client='%s' pid=%d</p><br>", k, v.gst_pid)
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
