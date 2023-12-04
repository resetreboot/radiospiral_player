package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// helper
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// JSON data we receive from the wp-json/radio/broadcast endpoint
type BroadcastResponse struct {
	Broadcast BroadcastInfo `json:"broadcast"`
	Updated   int           `json:"updated"`
	Success   bool          `json:"success"`
}

type BroadcastInfo struct {
	NowPlaying  NowPlayingInfo `json:"now_playing"`
	NextShow    NextShowInfo   `json:"next_show"`
	CurrentShow bool           `json:"current_show"`
}

type NowPlayingInfo struct {
	Text   string `json:"text"`
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

type NextShowInfo struct {
	Day   string   `json:"day"`
	Date  string   `json:"date"`
	Start string   `json:"start"`
	End   string   `json:"end"`
	Show  ShowData `json:"show"`
}

type ShowData struct {
	Name      string      `json:"name"`
	AvatarUrl string      `json:"avatar_url"`
	ImageUrl  string      `json:"image_url"`
	Hosts     []HostsData `json:"hosts"`
}

type HostsData struct {
	Name string `json:"name"`
}

// Radio player interface
type RadioPlayer interface {
	Play(stream_url string)
	Mute()
	Pause()
	IncVolume()
	DecVolume()
	Close()
}

// MPlayer
type MPlayer struct {
	player_name string
	is_playing  bool
	stream_url  string
	command     *exec.Cmd
	in          io.WriteCloser
	out         io.ReadCloser
	pipe_chan   chan io.ReadCloser
}

func (player *MPlayer) Play(stream_url string) {
	if !player.is_playing {
		var err error
		is_playlist := strings.HasSuffix(stream_url, ".m3u") || strings.HasSuffix(stream_url, ".pls")
		if is_playlist {
			// player.command = exec.Command(player.player_name, "-quiet", "-playlist", stream_url)
			player.command = exec.Command(player.player_name, "-playlist", stream_url)
		} else {
			player.command = exec.Command(player.player_name, stream_url)
		}
		player.in, err = player.command.StdinPipe()
		check(err)
		player.out, err = player.command.StdoutPipe()
		check(err)

		err = player.command.Start()
		check(err)

		player.is_playing = true
		player.stream_url = stream_url
		go func() {
			player.pipe_chan <- player.out
		}()
	}
}

func (player *MPlayer) Close() {
	if player.is_playing {
		player.is_playing = false

		player.in.Write([]byte("q"))
		player.in.Close()
		player.out.Close()
		player.command = nil

		player.stream_url = ""
	}
}

func (player *MPlayer) Mute() {
	if player.is_playing {
		player.in.Write([]byte("m"))
	}
}

func (player *MPlayer) Pause() {
	if player.is_playing {
		player.in.Write([]byte("p"))
	}
}

func (player *MPlayer) IncVolume() {
	if player.is_playing {
		player.in.Write([]byte("*"))
	}
}

func (player *MPlayer) DecVolume() {
	if player.is_playing {
		player.in.Write([]byte("/"))
	}
}

func main() {
	RADIOSPIRAL_JSON_ENDPOINT := "https://radiospiral.net/wp-json/radio/broadcast"

	status_chan := make(chan string)
	pipe_chan := make(chan io.ReadCloser)

	mplayer := MPlayer{player_name: "mplayer", is_playing: false, pipe_chan: pipe_chan}

	// Process the output of Mplayer here
	go func() {
		for {
			out_pipe := <-pipe_chan
			reader := bufio.NewReader(out_pipe)
			for {
				data, err := reader.ReadString('\n')
				if err != nil {
					status_chan <- "Playing stopped"
					break
				} else {
					status_chan <- data
				}
			}
		}
	}()

	app := app.New()
	window := app.NewWindow("RadioSpiral")

	window.Resize(fyne.NewSize(400, 600))

	play_status := false

	radiospiral_label := widget.NewLabel("RadioSpiral")
	nowplaying_label := widget.NewLabel("")

	radiospiral_label.Alignment = fyne.TextAlignCenter
	nowplaying_label.Alignment = fyne.TextAlignCenter

	var play_button *widget.Button
	play_button = widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() {
		if !mplayer.is_playing {
			play_button.SetIcon(theme.MediaPlayIcon())
			mplayer.Play("https://radiospiral.radio/stream.mp3")
			play_status = true
		} else {
			if play_status {
				play_status = false
				play_button.SetIcon(theme.MediaPauseIcon())
			} else {
				play_status = true
				play_button.SetIcon(theme.MediaPlayIcon())
			}
			mplayer.Pause()
		}
	})

	header := container.NewCenter(radiospiral_label)

	window.SetContent(container.NewVBox(
		header,
		layout.NewSpacer(),
		nowplaying_label,
		play_button,
	))

	go func() {
		for {
			fmt.Println("Retrieving broadcast data")
			resp, err := http.Get(RADIOSPIRAL_JSON_ENDPOINT)
			check(err)

			body, err := io.ReadAll(resp.Body)
			check(err)

			var broadcastResponse BroadcastResponse
			json.Unmarshal(body, &broadcastResponse)
			nowplaying_label.SetText("Now playing: " + broadcastResponse.Broadcast.NowPlaying.Text)
			time.Sleep(60 * time.Second)
		}
	}()

	window.ShowAndRun()
	mplayer.Close()
}
