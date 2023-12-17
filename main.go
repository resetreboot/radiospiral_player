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
	"path/filepath"
	"runtime"
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

// Enums and constants
const RADIOSPIRAL_STREAM = "https://radiospiral.radio/stream.mp3"
const RADIOSPIRAL_JSON_ENDPOINT = "https://radiospiral.net/wp-json/radio/broadcast"

const (
	Loading int = iota
	Playing
	Stopped
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
		ex, err := os.Executable()
		if err != nil {
			log.Fatal("Couldn't get executable path")
		}
		exPath := filepath.Dir(ex)
		PLAYER_CMD = filepath.Join(exPath, "ffmpeg.exe")
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
	// pipe_chan := make(chan io.ReadCloser)

	// Create our StreamPlayer instance
	streamPlayer := StreamPlayer{player_name: PLAYER_CMD}

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
		if !streamPlayer.IsPlaying() {
			playButton.SetIcon(theme.MediaStopIcon())
			playButton.SetText("(Buffering)")
			streamPlayer.Load(RADIOSPIRAL_STREAM)
			streamPlayer.Play()
			playStatus = Loading
		} else {
			if playStatus == Playing {
				playStatus = Stopped
				playButton.SetIcon(theme.MediaPlayIcon())
				streamPlayer.Stop()
			} else {
				playStatus = Loading
				playButton.SetText("(Buffering)")
				playButton.SetIcon(theme.MediaStopIcon())
				streamPlayer.Load(RADIOSPIRAL_STREAM)
				streamPlayer.Play()
			}
		}
	})

	// Process the output of ffmpeg here in a separate goroutine
	go func() {
		for {
			if streamPlayer.out != nil {
				for {
					var data [255]byte
					_, err := streamPlayer.out.Read(data[:])
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
			} else {
				// To avoid high CPU usage, we wait some milliseconds before testing
				// again for the change in streamPlayer.out from nil to ReadCloser
				time.Sleep(200 * time.Millisecond)
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
