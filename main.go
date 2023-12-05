package main

/*
 * This app is basically a frontend using MPlayer to stream, so we don't have to deal with
 * complicated streaming stuff when there's usually a perfectly working program that will
 * do this better than we can ever do.
 *
 * We keep control of MPlayer through the MPlayer object and pipes to send it commands to
 * play, stop and which is the URL we want to stream (usually, RadioSpiral's).
 *
 * There is also a goroutine that checks the broadcast information every minute, updates
 * the GUI with the currently playing information and also the next show coming up.
 */

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
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

// Load images from URLs
func loadImageURL(url string) image.Image {
	parts := strings.Split(url, "?")
	resp, err := http.Get(parts[0])
	check(err)

	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	check(err)
	return img
}

func main() {
	RADIOSPIRAL_JSON_ENDPOINT := "https://radiospiral.net/wp-json/radio/broadcast"

	// Create the status channel, to read from MPlayer and the pipe to send commands to it
	status_chan := make(chan string)
	pipe_chan := make(chan io.ReadCloser)

	// Create our MPlayer instance
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

	// Create our app and window
	app := app.New()
	window := app.NewWindow("RadioSpiral")

	window.Resize(fyne.NewSize(400, 600))

	// Keep the status of the player
	playStatus := false

	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")
	radioSpiralImage := canvas.NewImageFromImage(radioSpiralAvatar)
	radioSpiralImage.SetMinSize(fyne.NewSize(64, 64))
	radiospiralLabel := widget.NewLabel("RadioSpiral")
	nowPlayingLabel := widget.NewLabel("")

	radiospiralLabel.Alignment = fyne.TextAlignCenter
	nowPlayingLabel.Alignment = fyne.TextAlignCenter

	var playButton *widget.Button
	playButton = widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() {
		// Here we control each time the button is pressed and update its
		// appearance anytime it is clicked. We make the player start playing
		// or pause.
		if !mplayer.is_playing {
			playButton.SetIcon(theme.MediaPlayIcon())
			mplayer.Play("https://radiospiral.radio/stream.mp3")
			playStatus = true
		} else {
			if playStatus {
				playStatus = false
				playButton.SetIcon(theme.MediaPauseIcon())
			} else {
				playStatus = true
				playButton.SetIcon(theme.MediaPlayIcon())
			}
			mplayer.Pause()
		}
	})

	// Header section
	headerElems := container.NewHBox(radioSpiralImage, radiospiralLabel)
	header := container.NewCenter(headerElems)

	// Next show section
	showAvatar := canvas.NewImageFromImage(radioSpiralAvatar)
	showAvatar.SetMinSize(fyne.NewSize(200, 200))
	showNameLabel := widget.NewLabel("")
	showDate := widget.NewLabel("")
	showTimes := widget.NewLabel("")
	showHost := widget.NewLabel("")
	showInfo := container.NewVBox(showNameLabel, showDate, showTimes, showHost)

	showSection := container.NewHBox(showAvatar, showInfo)

	// Layout the whole thing
	window.SetContent(container.NewVBox(
		header,
		widget.NewLabel("Next show:"),
		showSection,
		layout.NewSpacer(),
		nowPlayingLabel,
		playButton,
	))

	// Now that everything is laid out, we can start this
	// small goroutine every minute, retrieve the stream data
	// and the shows data, update the GUI accordingly
	go func() {
		for {
			fmt.Println("Retrieving broadcast data")
			resp, err := http.Get(RADIOSPIRAL_JSON_ENDPOINT)
			check(err)

			body, err := io.ReadAll(resp.Body)
			check(err)

			var broadcastResponse BroadcastResponse
			json.Unmarshal(body, &broadcastResponse)
			nowPlayingLabel.SetText("Now playing: " + broadcastResponse.Broadcast.NowPlaying.Text)
			showNameLabel.SetText(broadcastResponse.Broadcast.NextShow.Show.Name)
			date := broadcastResponse.Broadcast.NextShow.Day + " " + broadcastResponse.Broadcast.NextShow.Date
			times := broadcastResponse.Broadcast.NextShow.Start + " to " + broadcastResponse.Broadcast.NextShow.End
			showDate.SetText(date)
			showTimes.SetText(times)
			showHost.SetText("by: " + broadcastResponse.Broadcast.NextShow.Show.Hosts[0].Name)
			fmt.Println(broadcastResponse.Broadcast.NextShow.Show.AvatarUrl)
			showAvatar.Image = loadImageURL(broadcastResponse.Broadcast.NextShow.Show.AvatarUrl)
			showAvatar.Refresh()
			time.Sleep(60 * time.Second)
		}
	}()

	// Showtime!
	window.ShowAndRun()

	// Window has been closed, make sure that MPlayer closes too
	mplayer.Close()
}
