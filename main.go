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
	"flag"
	"fmt"
	"log"
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
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Enums and constants
const MAX_CHARS = 24

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

func main() {
	// Here we store the current song, since we will be using in
	// several places
	var currentSong string
	var currentSongScrollIndex int

	stations, err := fetchStations()

	check(err)

	currentStation := stations[0]

	appRunning := true

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

	window.Resize(fyne.NewSize(400, 450))
	window.SetIcon(resourceIconPng)

	// Keep the status of the player
	playStatus := Stopped

	// Header section
	radioSpiralHeaderImage := canvas.NewImageFromResource(resourceHeaderPng)
	radioSpiralHeaderImage.SetMinSize(fyne.NewSize(400, 120))
	radioSpiralHeaderImage.FillMode = canvas.ImageFillContain

	// Placeholder avatar
	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")

	// Album cover section
	radioSpiralCanvas := canvas.NewImageFromImage(radioSpiralAvatar)
	radioSpiralCanvas.SetMinSize(fyne.NewSize(200, 200))
	albumCard := widget.NewCard("Now playing", "", radioSpiralCanvas)
	centerCardContainer := container.NewCenter(albumCard)

	volumeBind := binding.BindFloat(&streamPlayer.currentVolume)
	volumeBar := widget.NewProgressBarWithData(volumeBind)

	// Player section
	volumeDown := widget.NewButtonWithIcon("", theme.VolumeDownIcon(), func() {
		streamPlayer.DecVolume()
		volumeBind.Reload()
	})
	volumeUp := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		streamPlayer.IncVolume()
		volumeBind.Reload()
	})

	var volumeMute *widget.Button

	volumeMute = widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), func() {
		streamPlayer.Mute()
		if streamPlayer.IsMuted() {
			volumeMute.SetText("x")
		} else {
			volumeMute.SetText("")
		}
		volumeBind.Reload()
	})

	volumeTop := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		streamPlayer.SetVolume(1.0)
		streamPlayer.currentVolume = 1.0
		volumeBind.Reload()
	})

	// Station selector
	var stationSelect *widget.Select
	stationNames := make([]string, len(stations))

	for i, elem := range stations {
		stationNames[i] = elem.Name
	}

	stationSelect = widget.NewSelect(stationNames,
		func(r string) {
			idx := stationSelect.SelectedIndex()
			currentStation = stations[idx]

			if streamPlayer.IsPlaying() {
				volume := streamPlayer.GetVolume()
				streamPlayer.Stop()
				streamPlayer.Load(currentStation.ListenUrl)
				streamPlayer.Play()
				streamPlayer.SetVolume(volume)
				volumeBind.Reload()
			}
		})

	stationSelect.SetSelectedIndex(0)
	stationSelect.Resize(fyne.NewSize(300, 20))

	// Play button
	var playButton *widget.Button

	playButton = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
		// Here we control each time the button is pressed and update its
		// appearance anytime it is clicked. We make the player start playing
		// or pause.
		if !streamPlayer.IsPlaying() {
			playButton.SetIcon(theme.MediaStopIcon())
			playButton.SetText("(Buffering)")
			streamPlayer.Load(currentStation.ListenUrl)
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
				streamPlayer.Load(currentStation.ListenUrl)
				streamPlayer.Play()
			}
		}
		volumeBind.Reload()
	})

	playButton.Importance = widget.HighImportance

	volumeContainer := container.NewBorder(
		nil,
		nil,
		//	container.NewHBox(
		//		volumeMute,
		//		volumeDown,
		//	),
		//	container.NewHBox(
		//		volumeUp,
		//		volumeTop,
		//	),
		volumeDown,
		volumeUp,
		volumeBar,
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
							currentSong = newTitleParts[1]
							currentSongScrollIndex = 0
							albumCard.SetSubTitle(fmt.Sprintf("%.*s", MAX_CHARS, currentSong))
							stationData, err := queryStation(currentStation)
							if err != nil {
								log.Println("Received error")
								continue
							}

							// Cover art retrieval
							var coverArtURL string
							if stationData.Live.IsLive {
								log.Printf("Received %s as art", stationData.Live.Art)
								albumCard.SetTitle("Live Show")
								coverArtURL = stationData.Live.Art
							} else {
								log.Printf("Received %s as art", stationData.NowPlaying.Song.Art)
								albumCard.SetTitle("Now playing")
								coverArtURL = stationData.NowPlaying.Song.Art
							}

							if len(coverArtURL) > 0 {
								log.Println("Fetching album art")
								albumImg := loadImageURL(coverArtURL)
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

	controlContainer := container.NewBorder(
		nil,
		nil,
		volumeMute,
		volumeTop,
		playButton,
	)

	// Layout the whole thing
	window.SetContent(container.NewVBox(
		radioSpiralHeaderImage,
		container.NewCenter(widget.NewHyperlink("https://radiospiral.net", rsUrl)),
		container.NewPadded(stationSelect),
		centerCardContainer,
		volumeContainer,
		controlContainer,
	))

	// This small go routine will scroll the song title on the card if it is longer than MAX_CHARS
	go func() {
		for {
			if !appRunning {
				break
			}
			time.Sleep(1 * time.Second)
			if len(currentSong) > MAX_CHARS {
				topIndex := len(currentSong) - MAX_CHARS
				currentSongScrollIndex += 1
				if currentSongScrollIndex > topIndex {
					currentSongScrollIndex = 0
				}
				scrolledTitle := currentSong[currentSongScrollIndex : currentSongScrollIndex+MAX_CHARS]
				albumCard.SetSubTitle(scrolledTitle)
			}
		}
	}()

	// Showtime!
	window.ShowAndRun()
	appRunning = false
	streamPlayer.Close()
}
