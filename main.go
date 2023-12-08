//go:generate fyne bundle -o bundle.go res/icon.png
//go:generate fyne bundle -o bundle.go -append res/header.png

/*
 * Copyright 2023 José Carlos Cuevas
 *
 * This file is part of RadioSpiral Player.
 * RadioSpiral Player is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * RadioSpiral Player is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along with
 * RadioSpiral Player. If not, see <https://www.gnu.org/licenses/>.
 *
 */

package main

/*
 * This app is basically using ffmpeg to read the stream, so we don't have to deal with
 * complicated streaming stuff when there's usually a perfectly working program that will
 * do this better than we can ever do.
 *
 * We use the Oto library (multiplatform!) to send the raw WAV data from ffmpeg to the audio
 * system of the OS
 *
 * We keep control of oto and ffmpeg through the StreamPlayer object. We use pipe of stderr
 * to read the text output of ffmpeg and watch for stream title changes, indside a goroutine
 *
 * There is also a goroutine that checks the broadcast information every ten minutes, updates
 * the GUI with the currently playing information and also the next show coming up.
 */

import (
	"encoding/json"
	"flag"
	"image"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/muesli/cancelreader"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Enums and constants
const RADIOSPIRAL_STREAM = "https://radiospiral.radio/stream.mp3"
const RADIOSPIRAL_JSON_ENDPOINT = "https://radiospiral.net/wp-json/radio/broadcast"

const (
	Loading int = iota
	Playing
	Paused
	Stopped
)

// Cancel Reader

var reader cancelreader.CancelReader

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
	Load(stream_url string)
	IsPlaying() bool
	Play()
	Mute()
	Pause()
	IncVolume()
	DecVolume()
	Close()
}

// StreamPlayer
type StreamPlayer struct {
	player_name   string
	stream_url    string
	command       *exec.Cmd
	in            io.WriteCloser
	out           io.ReadCloser
	audio         io.ReadCloser
	pipe_chan     chan io.ReadCloser
	otoContext    *oto.Context
	otoPlayer     *oto.Player
	currentVolume float64
	paused        bool
}

func (player *StreamPlayer) IsPlaying() bool {
	if player.otoPlayer == nil {
		log.Println("Player not loaded!")
		return false
	}

	return player.otoPlayer.IsPlaying()
}

func (player *StreamPlayer) Load(stream_url string) {
	if (player.otoPlayer == nil) || (!player.otoPlayer.IsPlaying()) {
		var err error
		is_playlist := strings.HasSuffix(stream_url, ".m3u") || strings.HasSuffix(stream_url, ".pls")
		if is_playlist {
			// TODO: Check ffmpeg's ability to deal with playlists
			// player.command = exec.Command(player.player_name, "-quiet", "-playlist", stream_url)
			player.command = exec.Command(player.player_name, "-nodisp", "-loglevel", "verbose", "-playlist", stream_url)
		} else {
			player.command = exec.Command(player.player_name, "-loglevel", "verbose", "-i", stream_url, "-f", "wav", "-")
		}

		// In to send things over stdin to ffmpeg
		player.in, err = player.command.StdinPipe()
		check(err)
		// Out will be the wave data we will read and play
		player.audio, err = player.command.StdoutPipe()
		check(err)
		// Err is the output of ffmpeg, used to get stream title
		player.out, err = player.command.StderrPipe()
		check(err)

		log.Println("Starting ffmpeg")
		err = player.command.Start()
		check(err)

		player.stream_url = stream_url

		op := &oto.NewContextOptions{
			SampleRate:   44100,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		}

		if player.otoContext == nil {
			otoContext, readyChan, err := oto.NewContext(op)
			player.otoContext = otoContext
			if err != nil {
				log.Fatal(err)
			}
			<-readyChan
		}

		player.otoPlayer = player.otoContext.NewPlayer(player.audio)
		// Save current volume for the mute function
		player.currentVolume = player.otoPlayer.Volume()

		player.paused = false

		go func() {
			player.pipe_chan <- player.out
		}()
	}
}

func (player *StreamPlayer) Play() {
	if player.otoPlayer == nil {
		log.Println("Stream not loaded")
		return
	}

	if !player.otoPlayer.IsPlaying() {
		if player.command == nil {
			player.Load(player.stream_url)
		}
		player.otoPlayer.Play()
	}
}

func (player *StreamPlayer) Close() {
	if player.IsPlaying() {
		err := player.otoPlayer.Close()
		if err != nil {
			log.Println(err)
		}
		player.in.Close()
		player.out.Close()
		player.audio.Close()

		player.stream_url = ""
	}
}

func (player *StreamPlayer) Mute() {
	if player.IsPlaying() {
		if player.otoPlayer.Volume() > 0 {
			player.currentVolume = player.otoPlayer.Volume()
			player.otoPlayer.SetVolume(0.0)
		} else {
			player.otoPlayer.SetVolume(player.currentVolume)
		}
	}
}

func (player *StreamPlayer) Pause() {
	if player.IsPlaying() {
		if !player.paused {
			player.paused = true
			player.otoPlayer.Pause()
		}
	}
}

func (player *StreamPlayer) IncVolume() {
	if player.IsPlaying() {
		player.currentVolume += 0.05
		if player.currentVolume >= 1.0 {
			player.currentVolume = 1.0
		}
		player.otoPlayer.SetVolume(player.currentVolume)
	}
}

func (player *StreamPlayer) DecVolume() {
	if player.IsPlaying() {
		player.currentVolume -= 0.05
		if player.currentVolume <= 0.0 {
			player.currentVolume = 0.0
		}
		player.otoPlayer.SetVolume(player.currentVolume)
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
	PLAYER_CMD := "ffmpeg"

	if runtime.GOOS == "windows" {
		log.Println("Detected Windows")
		PLAYER_CMD = "ffmpeg.exe"
	}

	// Command line arguments parsing
	loggingToFilePtr := flag.Bool("log", false, "Create a log file")

	flag.Parse()

	if *loggingToFilePtr {
		logFile, err := os.OpenFile("radiospiral.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		check(err)
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	log.Println("Starting the app")

	// Create the status channel, to read from StreamPlayer and the pipe to send commands to it
	pipe_chan := make(chan io.ReadCloser)

	// Create our StreamPlayer instance
	streamPlayer := StreamPlayer{player_name: PLAYER_CMD, pipe_chan: pipe_chan}

	// Create our app and window
	app := app.New()
	window := app.NewWindow("RadioSpiral Player")

	window.Resize(fyne.NewSize(400, 600))
	window.SetIcon(resourceIconPng)

	// Keep the status of the player
	playStatus := Stopped

	// Placeholder avatar
	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")

	// Header section
	radioSpiralHeaderImage := canvas.NewImageFromResource(resourceHeaderPng)
	radioSpiralHeaderImage.SetMinSize(fyne.NewSize(400, 120))
	radioSpiralHeaderImage.FillMode = canvas.ImageFillContain

	// Next show section
	showAvatar := canvas.NewImageFromImage(radioSpiralAvatar)
	showAvatar.SetMinSize(fyne.NewSize(200, 200))
	showCard := widget.NewCard("RadioSpiral", "", showAvatar)
	centerCardContainer := container.NewCenter(showCard)

	// Player section
	nowPlayingLabelHeader := widget.NewLabel("Now playing:")
	nowPlayingLabel := widget.NewLabel("")
	volumeDown := widget.NewButtonWithIcon("", theme.VolumeDownIcon(), func() {
		streamPlayer.DecVolume()
	})
	volumeUp := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		streamPlayer.IncVolume()
	})
	volumeMute := widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), func() {
		streamPlayer.Mute()
	})
	controlContainer := container.NewHBox(
		nowPlayingLabelHeader,
		layout.NewSpacer(),
		volumeDown,
		volumeUp,
		volumeMute,
	)

	nowPlayingLabel.Alignment = fyne.TextAlignCenter
	nowPlayingLabel.Wrapping = fyne.TextWrapWord

	var playButton *widget.Button

	playButton = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
		// Here we control each time the button is pressed and update its
		// appearance anytime it is clicked. We make the player start playing
		// or pause.
		if !streamPlayer.IsPlaying() && !streamPlayer.paused {
			playButton.SetIcon(theme.MediaPauseIcon())
			playButton.SetText("(Buffering)")
			streamPlayer.Load(RADIOSPIRAL_STREAM)
			streamPlayer.Play()
			playStatus = Loading
		} else {
			if playStatus == Playing {
				playStatus = Paused
				playButton.SetIcon(theme.MediaPlayIcon())
				streamPlayer.Pause()
			} else {
				reader.Cancel()
				playStatus = Loading
				playButton.SetText("(Buffering)")
				playButton.SetIcon(theme.MediaPauseIcon())
				streamPlayer.Load(RADIOSPIRAL_STREAM)
				streamPlayer.Play()
			}
		}
	})

	// Process the output of ffmpeg here in a separate goroutine
	go func() {
		for {
			out_pipe := <-pipe_chan
			var err error
			reader, err = cancelreader.NewReader(out_pipe)
			if err != nil {
				log.Println("Error opening reader")
			}
			for {
				var data [255]byte
				_, err := reader.Read(data[:])
				if err != nil {
					log.Println(err)
					break
				}
				lines := strings.Split(string(data[:]), "\n")
				for _, line := range lines {
					// Log, if enabled, the output of StreamPlayer
					if *loggingToFilePtr {
						log.Print("[" + streamPlayer.player_name + "] " + line)
					}
					if strings.Contains(line, "Output #0") {
						playStatus = Playing
						playButton.SetText("")
					}
					// Check if there's an updated title and reflect it on the
					// GUI
					if strings.Contains(line, "StreamTitle: ") {
						log.Println("Found new stream title, updating GUI")
						newTitleParts := strings.Split(line, "StreamTitle: ")
						nowPlayingLabel.SetText(newTitleParts[1])
					}
				}
			}
		}
	}()

	rsUrl, err := url.Parse("https://radiospiral.net")

	if err != nil {
		log.Println("Error generating RadioSpiral url")
	}

	// Layout the whole thing
	window.SetContent(container.NewVBox(
		radioSpiralHeaderImage,
		container.NewCenter(widget.NewHyperlink("https://radiospiral.net", rsUrl)),
		layout.NewSpacer(),
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
	streamPlayer.Close()
}
