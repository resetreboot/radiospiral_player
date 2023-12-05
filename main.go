//go:generate fyne bundle -o bundle.go res/icon.png
//go:generate fyne bundle -o bundle.go -append res/header.png

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
	"image"
	"io"
	"log"
	"net/http"
	"os"
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
		log.Panic(err)
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
			player.command = exec.Command(player.player_name, "-v", "-playlist", stream_url)
		} else {
			player.command = exec.Command(player.player_name, "-v", stream_url)
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
	RADIOSPIRAL_STREAM := "https://radiospiral.radio/stream.mp3"
	RADIOSPIRAL_JSON_ENDPOINT := "https://radiospiral.net/wp-json/radio/broadcast"

	logFile, err := os.OpenFile("radiospiral.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	check(err)
	defer logFile.Close()

	log.SetOutput(logFile)
	log.Println("Starting the app")

	// Create the status channel, to read from MPlayer and the pipe to send commands to it
	pipe_chan := make(chan io.ReadCloser)

	// Create our MPlayer instance
	mplayer := MPlayer{player_name: "mplayer", is_playing: false, pipe_chan: pipe_chan}

	// Make sure that MPlayer closes when the program ends
	defer mplayer.Close()

	// Create our app and window
	app := app.New()
	window := app.NewWindow("RadioSpiral Player")

	window.Resize(fyne.NewSize(400, 600))
	window.SetIcon(resourceIconPng)

	// Keep the status of the player
	playStatus := false

	// Placeholder avatar
	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")

	// Header section
	radioSpiralImage := canvas.NewImageFromResource(resourceHeaderPng)
	header := container.NewCenter(radioSpiralImage)

	// Next show section
	showAvatar := canvas.NewImageFromImage(radioSpiralAvatar)
	showAvatar.SetMinSize(fyne.NewSize(200, 200))
	showCard := widget.NewCard("RadioSpiral", "", showAvatar)
	centerCardContainer := container.NewCenter(showCard)

	radioSpiralImage.SetMinSize(fyne.NewSize(400, 120))
	nowPlayingLabelHeader := widget.NewLabel("Now playing:")
	nowPlayingLabel := widget.NewLabel("")
	var playButton *widget.Button
	volumeDown := widget.NewButtonWithIcon("", theme.VolumeDownIcon(), func() {
		mplayer.DecVolume()
	})
	volumeUp := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		mplayer.IncVolume()
	})
	controlContainer := container.NewHBox(
		nowPlayingLabelHeader,
		layout.NewSpacer(),
		volumeDown,
		volumeUp,
	)

	nowPlayingLabel.Alignment = fyne.TextAlignCenter
	nowPlayingLabel.Wrapping = fyne.TextWrapWord

	// Process the output of Mplayer here in a separate goroutine
	go func() {
		for {
			out_pipe := <-pipe_chan
			reader := bufio.NewReader(out_pipe)
			for {
				data, err := reader.ReadString('\n')
				if err != nil {
					log.Fatal(err)
					log.Println("Reloading player")
					mplayer.Close()
					pipe_chan = make(chan io.ReadCloser)
					mplayer = MPlayer{player_name: "mplayer", is_playing: false, pipe_chan: pipe_chan}
					mplayer.Play(RADIOSPIRAL_STREAM)
					playStatus = true
					playButton.SetIcon(theme.MediaPlayIcon())
					defer mplayer.Close()
				} else {
					// Check if there's an updated title and reflect it on the
					// GUI
					log.Print("[mplayer] " + data)
					if strings.Contains(data, "StreamTitle: ") {
						log.Println("Found new stream title, updating GUI")
						newTitleParts := strings.Split(data, "StreamTitle: ")
						nowPlayingLabel.SetText(newTitleParts[1])
					}
				}
			}
		}
	}()

	playButton = widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() {
		// Here we control each time the button is pressed and update its
		// appearance anytime it is clicked. We make the player start playing
		// or pause.
		if !mplayer.is_playing {
			playButton.SetIcon(theme.MediaPlayIcon())
			mplayer.Play(RADIOSPIRAL_STREAM)
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

	// Layout the whole thing
	window.SetContent(container.NewVBox(
		header,
		widget.NewLabel("Next show:"),
		centerCardContainer,
		layout.NewSpacer(),
		controlContainer,
		nowPlayingLabel,
		playButton,
	))

	// Now that everything is laid out, we can start this
	// small goroutine every minute, retrieve the stream data
	// and the shows data, update the GUI accordingly
	go func() {
		for {
			resp, err := http.Get(RADIOSPIRAL_JSON_ENDPOINT)
			if err != nil {
				// If we get an error fetching the data, await a minute and retry
				log.Println("[ERROR] Error when querying broadcast endpoint")
				log.Println(err)
				time.Sleep(1 * time.Minute)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				// We couldn't read the body, log the error, await a minute and retry
				log.Println("[ERROR] Error when reading the body")
				log.Println(err)
				time.Sleep(1 * time.Minute)
				continue
			}

			var broadcastResponse BroadcastResponse

			json.Unmarshal(body, &broadcastResponse)
			showCard.SetTitle(broadcastResponse.Broadcast.NextShow.Show.Name)
			date := broadcastResponse.Broadcast.NextShow.Day + " " + broadcastResponse.Broadcast.NextShow.Date
			host := "by: " + broadcastResponse.Broadcast.NextShow.Show.Hosts[0].Name
			showCard.SetSubTitle(date + " " + host)
			showAvatar.Image = loadImageURL(broadcastResponse.Broadcast.NextShow.Show.AvatarUrl)
			showAvatar.Refresh()
			time.Sleep(10 * time.Minute)
		}
	}()

	// Showtime!
	window.ShowAndRun()
}
