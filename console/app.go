package app

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"text/template"

	"github.com/gorilla/websocket"
	"github.com/kr/pty"
	"github.com/yudai/umutex"
)

type App struct {
	command []string
	options *Options

	upgrader *websocket.Upgrader

	titleTemplate *template.Template

	onceMutex *umutex.UnblockingMutex
}

type Options struct {
	Address         string                 `hcl:"address"`
	Port            string                 `hcl:"port"`
	PermitWrite     bool                   `hcl:"permit_write"`
	IndexFile       string                 `hcl:"index_file"`
	TitleFormat     string                 `hcl:"title_format"`
	EnableReconnect bool                   `hcl:"enable_reconnect"`
	ReconnectTime   int                    `hcl:"reconnect_time"`
	PermitArguments bool                   `hcl:"permit_arguments"`
	CloseSignal     int                    `hcl:"close_signal"`
	Preferences     HtermPrefernces        `hcl:"preferences"`
	RawPreferences  map[string]interface{} `hcl:"preferences"`
	SessionKey      string                 `hcl:"session_key"`
}

var Version = "0.0.1"

var DefaultOptions = Options{
	Address:         "",
	Port:            "8080",
	PermitWrite:     true,
	IndexFile:       "",
	TitleFormat:     "GRTTY Command",
	EnableReconnect: true,
	ReconnectTime:   10,
	CloseSignal:     1, // syscall.SIGHUP
	Preferences:     HtermPrefernces{},
	SessionKey:      "_auth_user_id",
}

type InitMessage struct {
	T_id string `json:t_id`
	S_id string `json:s_id`
	C_id string `json:c_id`
	Md5  string `json:md5`
}

func checkSameOrigin(r *http.Request) bool {
	return true
}

func New(command []string, options *Options) (*App, error) {
	titleTemplate, _ := template.New("title").Parse(options.TitleFormat)
	return &App{
		command: command,
		options: options,
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     checkSameOrigin,
		},
		titleTemplate: titleTemplate,
		onceMutex:     umutex.New(),
	}, nil
}

func (app *App) Run() error {

	path := ""
	endpoint := net.JoinHostPort(app.options.Address, app.options.Port)

	wsHandler := http.HandlerFunc(app.handleWS)

	var siteMux = http.NewServeMux()

	siteHandler := http.Handler(siteMux)

	siteHandler = wrapHeaders(siteHandler)

	wsMux := http.NewServeMux()
	wsMux.Handle("/", siteHandler)
	wsMux.Handle(path+"/ws", wsHandler)
	siteHandler = (http.Handler(wsMux))

	siteHandler = wrapLogger(siteHandler)

	server, err := app.makeServer(endpoint, &siteHandler)
	if err != nil {
		return errors.New("Failed to build server: " + err.Error())
	}

	log.Fatal(server.ListenAndServe())

	log.Printf("Exiting...")

	return nil
}

func (app *App) makeServer(addr string, handler *http.Handler) (*http.Server, error) {
	server := &http.Server{
		Addr:    addr,
		Handler: *handler,
	}

	return server, nil
}

func (app *App) handleWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("New client connected: %s", r.RemoteAddr)

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	conn, err := app.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Failed to upgrade connection: " + err.Error())
		return
	}

	_, stream, err := conn.ReadMessage()
	if err != nil {
		log.Print("Failed to authenticate websocket connection " + err.Error())
		conn.Close()
		return
	}

	message := string(stream)
	log.Print("message=", message)

	var init InitMessage

	err = json.Unmarshal(stream, &init)

	//todo auth
	if init.C_id == "" {
		log.Print("Parameter is error:" + init.C_id)
		conn.WriteMessage(websocket.TextMessage, []byte("Parameter is error !"))
		conn.Close()
		return
	}
	key := init.T_id + "_" + init.S_id + "_" + init.C_id
	log.Print(key)
	md5 := md5Func(key)
	if md5 != init.Md5 {
		log.Print("Auth is not allowed !")
		conn.WriteMessage(websocket.TextMessage, []byte("Auth is not allowed!"))
		conn.Close()
		return
	}

	cmd := exec.Command("docker", "exec", "-ti", init.C_id, "/bin/bash")
	ptyIo, err := pty.Start(cmd)
	if err != nil {
		log.Print("Failed to execute command")
		return
	}
	log.Printf("Command is running for client %s with PID %d ", r.RemoteAddr, cmd.Process.Pid)

	context := &clientContext{
		app:        app,
		request:    r,
		connection: conn,
		command:    cmd,
		pty:        ptyIo,
		writeMutex: &sync.Mutex{},
	}

	context.goHandleClient()
}

func (app *App) Exit() (firstCall bool) {
	return true
}

func wrapLogger(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWrapper{w, 200}
		handler.ServeHTTP(rw, r)
		log.Printf("%s %d %s %s", r.RemoteAddr, rw.status, r.Method, r.URL.Path)
	})
}

func wrapHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "GoTTY/"+Version)
		handler.ServeHTTP(w, r)
	})
}

func md5Func(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	cipherStr := h.Sum(nil)
	return hex.EncodeToString(cipherStr)
}
