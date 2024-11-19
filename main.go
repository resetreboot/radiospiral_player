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
const RADIOSPIRAL_STREAM = "https://radiospiral.radio:8000/stream.mp3"
const RADIOSPIRAL_SCHEDULE = "https://radiospiral.radio/api/station/radiospiral/schedule"
const RADIOSPIRAL_NOWPLAYING = "https://radiospiral.radio/api/nowplaying/radiospiral"

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
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	StartTime   int64  `json:"start_timestamp"`
	IsNow       bool   `json:"is_now"`
}

type StationResponse struct {
	NowPlaying NowPlayingInfo `json:"now_playing"`
	Listeners  ListenersInfo  `json:"listeners"`
	Live       LiveInfo       `json:"live"`
}

type ListenersInfo struct {
	Total   int `json:"total"`
	Unique  int `json:"unique"`
	Current int `json:"current"`
}

type LiveInfo struct {
	IsLive         bool   `json:"is_live"`
	StreamerName   string `json:"streamer_name"`
	BroadcastStart string `json:"broadcast_start"`
	Art            string `json:"art"`
}

type SongInfo struct {
	Id     string `json:"id"`
	Text   string `json:"text"`
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Album  string `json:"album"`
	Genre  string `json:"genre"`
	Isrc   string `json:"isrc"`
	Lyrics string `json:"lyrics"`
	Art    string `json:"art"`
}

type NowPlayingInfo struct {
	ShId      int      `json:"sh_id"`
	PlayedAt  int64    `json:"played_at"`
	Duration  int      `json:"duration"`
	Playlist  string   `json:"playlist"`
	Streamer  string   `json:"streamer"`
	IsRequest bool     `json:"is_request"`
	Song      SongInfo `json:"song"`
	Elapsed   int      `json:"elapsed"`
	Remaining int      `json:"remaining"`
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

// Query the station info
func queryStation() (*StationResponse, error) {
	resp, err := http.Get(RADIOSPIRAL_NOWPLAYING)
	if err != nil {
		// If we get an error fetching the data, await a minute and retry
		log.Println("[ERROR] Error when querying broadcast endpoint")
		log.Println(err)
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()

	if err != nil {
		// We couldn't read the body, log the error, await a minute and retry
		log.Println("[ERROR] Error when reading the body")
		log.Println(err)
		return nil, err
	}

	var response StationResponse
	json.Unmarshal(body, &response)

	return &response, nil
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

	// Header section
	radioSpiralHeaderImage := canvas.NewImageFromResource(resourceHeaderPng)
	radioSpiralHeaderImage.SetMinSize(fyne.NewSize(400, 120))
	radioSpiralHeaderImage.FillMode = canvas.ImageFillContain

	// Next show
	nextShowHeader := widget.NewRichTextFromMarkdown("# Next Show:")
	// nextShowHeader := widget.NewLabel("Next show:")
	nextShowLabel := widget.NewRichTextFromMarkdown("")
	nextShowDate := widget.NewLabel("")

	nextShowLabelContainer := container.NewHBox(
		layout.NewSpacer(),
		nextShowLabel,
		layout.NewSpacer(),
	)

	nextShowDateContainer := container.NewHBox(
		layout.NewSpacer(),
		nextShowDate,
		layout.NewSpacer(),
	)

	nextShowDate.Alignment = fyne.TextAlignCenter
	nextShowDate.Wrapping = fyne.TextWrapWord

	// Placeholder avatar
	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")

	// Album cover section
	radioSpiralCanvas := canvas.NewImageFromImage(radioSpiralAvatar)
	radioSpiralCanvas.SetMinSize(fyne.NewSize(200, 200))
	albumCard := widget.NewCard("Now playing", "", radioSpiralCanvas)
	centerCardContainer := container.NewCenter(albumCard)

	// Player section
	volumeDown := widget.NewButtonWithIcon("", theme.VolumeDownIcon(), func() {
		streamPlayer.DecVolume()
	})
	volumeUp := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		streamPlayer.IncVolume()
	})

	var volumeMute *widget.Button

	volumeMute = widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), func() {
		streamPlayer.Mute()
		if streamPlayer.IsMuted() {
			volumeMute.SetText("x")
		} else {
			volumeMute.SetText("")
		}
	})

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

	playButton.Importance = widget.HighImportance

	controlContainer := container.NewHBox(
		layout.NewSpacer(),
		volumeDown,
		volumeUp,
		volumeMute,
		layout.NewSpacer(),
	)

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
							albumCard.SetSubTitle(newTitleParts[1])
							stationData, err := queryStation()
							if err != nil {
								log.Println("Received error")
								continue
							}
							log.Printf("Received %s as art", stationData.NowPlaying.Song.Art)
							if len(stationData.NowPlaying.Song.Art) > 0 {
								log.Println("Fetching album art")
								albumImg := loadImageURL(stationData.NowPlaying.Song.Art)
								albumCanvas := canvas.NewImageFromImage(albumImg)
								albumCanvas.SetMinSize(fyne.NewSize(200, 200))
								albumCard.SetContent(albumCanvas)
							} else {
								albumCanvas := canvas.NewImageFromImage(radioSpiralAvatar)
								albumCanvas.SetMinSize(fyne.NewSize(200, 200))
								albumCard.SetContent(albumCanvas)
							}
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
		nextShowHeader,
		nextShowLabelContainer,
		nextShowDateContainer,
		layout.NewSpacer(),
		centerCardContainer,
		controlContainer,
		playButton,
	))

	// Now that everything is laid out, we can start this
	// small goroutine every minute, retrieve the stream data
	// and the shows data, update the GUI accordingly
	go func() {
		for {
			resp, err := http.Get(RADIOSPIRAL_SCHEDULE)
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

			var broadcastResponse []BroadcastResponse

			json.Unmarshal(body, &broadcastResponse)

			var nextShow BroadcastResponse

			for _, elem := range broadcastResponse {
				if elem.Type != "playlist" {
					nextShow = elem
					break
				}
			}

			host := ""
			if len(nextShow.Name) > 0 {
				host = "by: " + nextShow.Name
				date := time.Unix(nextShow.StartTime, 0)
				dateString := date.Format(time.RFC850)
				nextShowLabel.ParseMarkdown("## " + nextShow.Name + " " + host)
				nextShowDate.SetText(dateString + " " + host)
			} else {
				nextShowLabel.ParseMarkdown("## Spud")
				nextShowDate.SetText("")
			}
			time.Sleep(10 * time.Minute)
		}
	}()

	// Showtime!
	window.ShowAndRun()
	streamPlayer.Close()
}
