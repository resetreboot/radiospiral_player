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
	"bufio"
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

// App status information goes into this struct
type AppStatus struct {
	AppRunning             bool
	Stations               []StationInfo
	CurrentStation         StationInfo
	StreamPlayer           StreamPlayer
	CurrentSong            string
	CurrentSongScrollIndex int
	PlayStatus             int
}

// helper
func check(err error) {
	if err != nil {
		log.Panic(err)
	}
}

func initLogging() (*os.File, error) {
	// Use home app data directory instead of CWD
	homeDir, _ := os.UserHomeDir()
	logPath := filepath.Join(homeDir, ".local/share/radiospiral", "radiospiral.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	log.SetOutput(file)
	return file, nil
}

func main() {
	// Create the main App struct to hold all the necessary data that
	// we need to keep the app status shared
	appStatus := AppStatus{}

	// Logfile
	var logFile *os.File

	appStatus.AppRunning = true

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
	testStation := flag.Bool("test", false, "")

	flag.Parse()

	var err error

	if *loggingToFilePtr {
		logFile, err = initLogging()
		if err != nil {
			fmt.Println("WARNING: Couldn't create the log file")
		}
	}

	log.Println("Starting the app")

	log.Println("Fetching the stations configuration")
	appStatus.Stations, err = fetchStations(*testStation)
	check(err)

	appStatus.CurrentStation = appStatus.Stations[0]

	// Create the status channel, to read from StreamPlayer and the pipe to send commands to it
	// pipe_chan := make(chan io.ReadCloser)

	// Create our StreamPlayer instance
	appStatus.StreamPlayer = StreamPlayer{player_name: PLAYER_CMD}

	// Create our app and window
	app := app.New()
	window := app.NewWindow("RadioSpiral Player")
	aboutWindow := app.NewWindow("About RadioSpiral")

	// Placeholder avatar
	radioSpiralAvatar := loadImageURL("https://radiospiral.net/wp-content/uploads/2018/03/Radio-Spiral-Logo-1.png")

	// Album cover section
	radioSpiralCanvas := canvas.NewImageFromImage(radioSpiralAvatar)
	radioSpiralCanvas.SetMinSize(fyne.NewSize(200, 200))
	albumCard := widget.NewCard("Now playing", "", radioSpiralCanvas)

	playButton, err := createMainWindow(window, aboutWindow, albumCard, &appStatus)

	if err != nil {
		log.Fatal("Couldn't create main window")
	}

	// Keep the status of the player
	appStatus.PlayStatus = Stopped

	// Process the output of ffmpeg here in a separate goroutine
	go func() {
		var scanner *bufio.Scanner
		for {
			if appStatus.StreamPlayer.out != nil {
				scanner = bufio.NewScanner(appStatus.StreamPlayer.out)
			} else {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			for scanner.Scan() {
				line := scanner.Text()
				// Log, if enabled, the output of StreamPlayer
				if *loggingToFilePtr {
					log.Print("[" + appStatus.StreamPlayer.player_name + "] " + line)
				}
				if strings.Contains(line, "Output #0") {
					appStatus.PlayStatus = Playing
					playButton.SetText("")
				}
				// Check if there's an updated title and reflect it on the
				// GUI
				if strings.Contains(line, "StreamTitle: ") {
					log.Println("Found new stream title, updating GUI")
					newTitleParts := strings.Split(line, "StreamTitle: ")
					appStatus.CurrentSong = newTitleParts[1]
					appStatus.CurrentSongScrollIndex = 0
					albumCard.SetSubTitle(fmt.Sprintf("%.*s", MAX_CHARS, appStatus.CurrentSong))
					stationData, err := queryStation(appStatus.CurrentStation)
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
			if err := scanner.Err(); err != nil {
				log.Println("FFMpeg stream not ready or ended. Waiting before restarting")
				time.Sleep(200 * time.Millisecond)
				scanner = bufio.NewScanner(appStatus.StreamPlayer.out)
			}
		}
	}()

	// This small go routine will scroll the song title on the card if it is longer than MAX_CHARS
	go func() {
		forwardDir := true
		for {
			if !appStatus.AppRunning {
				break
			}
			time.Sleep(400 * time.Millisecond)
			if len(appStatus.CurrentSong) > MAX_CHARS {
				topIndex := len(appStatus.CurrentSong) - MAX_CHARS
				if forwardDir {
					appStatus.CurrentSongScrollIndex += 1
					if appStatus.CurrentSongScrollIndex > topIndex {
						appStatus.CurrentSongScrollIndex = topIndex
						time.Sleep(1 * time.Second)
						forwardDir = false
					}
				} else {
					appStatus.CurrentSongScrollIndex -= 1
					if appStatus.CurrentSongScrollIndex < 0 {
						appStatus.CurrentSongScrollIndex = 0
						time.Sleep(1 * time.Second)
						forwardDir = true
					}
				}
				scrolledTitle := appStatus.CurrentSong[appStatus.CurrentSongScrollIndex : appStatus.CurrentSongScrollIndex+MAX_CHARS]
				albumCard.SetSubTitle(scrolledTitle)
			} else {
				appStatus.CurrentSongScrollIndex = 0
			}
		}
	}()

	// If the window is closed, clean all stuff
	window.SetOnClosed(func() {
		// Make sure we stop the player
		if appStatus.StreamPlayer.IsPlaying() {
			appStatus.StreamPlayer.Stop()
		}
		// Give it some time to stop
		time.Sleep(100 * time.Millisecond)
		appStatus.StreamPlayer.Close()
		// And to close properly
		time.Sleep(100 * time.Millisecond)
		appStatus.AppRunning = false
		if logFile != nil {
			defer logFile.Close()
		}
	})

	// Showtime!
	window.ShowAndRun()
}

// createMainWindow constructs the UI for the main app window
func createMainWindow(window fyne.Window,
	aboutWindow fyne.Window,
	albumCard *widget.Card,
	appStatus *AppStatus) (*widget.Button, error) {

	window.Resize(fyne.NewSize(400, 450))
	window.SetIcon(resourceIconPng)

	// Header section
	radioSpiralHeaderImage := canvas.NewImageFromResource(resourceHeaderPng)
	radioSpiralHeaderImage.SetMinSize(fyne.NewSize(400, 120))
	radioSpiralHeaderImage.FillMode = canvas.ImageFillContain

	createAboutWindow(aboutWindow)

	aboutButton := widget.NewButton("About us", func() {
		aboutWindow.Show()
	})

	centerCardContainer := container.NewCenter(albumCard)

	volumeBind := binding.BindFloat(&appStatus.StreamPlayer.currentVolume)
	volumeBar := widget.NewProgressBarWithData(volumeBind)

	// Player section
	volumeDown := widget.NewButtonWithIcon("", theme.VolumeDownIcon(), func() {
		appStatus.StreamPlayer.DecVolume()
		volumeBind.Reload()
	})
	volumeUp := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		appStatus.StreamPlayer.IncVolume()
		volumeBind.Reload()
	})

	var volumeMute *widget.Button

	volumeMute = widget.NewButtonWithIcon("", theme.VolumeMuteIcon(), func() {
		appStatus.StreamPlayer.Mute()
		if appStatus.StreamPlayer.IsMuted() {
			volumeMute.SetText("x")
		} else {
			volumeMute.SetText("")
		}
		volumeBind.Reload()
	})

	volumeTop := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		appStatus.StreamPlayer.SetVolume(1.0)
		appStatus.StreamPlayer.currentVolume = 1.0
		volumeBind.Reload()
	})

	// Station selector
	var stationSelect *widget.Select
	stationNames := make([]string, len(appStatus.Stations))

	for i, elem := range appStatus.Stations {
		stationNames[i] = elem.Name
	}

	stationSelect = widget.NewSelect(stationNames,
		func(r string) {
			idx := stationSelect.SelectedIndex()
			appStatus.CurrentStation = appStatus.Stations[idx]

			if appStatus.StreamPlayer.IsPlaying() {
				volume := appStatus.StreamPlayer.GetVolume()
				appStatus.StreamPlayer.Stop()
				appStatus.StreamPlayer.Load(appStatus.CurrentStation.ListenUrl)
				appStatus.StreamPlayer.Play()
				appStatus.StreamPlayer.SetVolume(volume)
				volumeBind.Reload()
			}
		})

	stationSelect.SetSelectedIndex(0)
	stationSelect.Resize(fyne.NewSize(300, 20))

	if len(appStatus.Stations) == 1 {
		// No need to show extra stations if they are not present
		stationSelect.Hide()
	}

	// Play button
	var playButton *widget.Button

	playButton = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
		// Here we control each time the button is pressed and update its
		// appearance anytime it is clicked. We make the player start playing
		// or pause.
		if !appStatus.StreamPlayer.IsPlaying() {
			playButton.SetIcon(theme.MediaStopIcon())
			playButton.SetText("(Buffering)")
			appStatus.StreamPlayer.Load(appStatus.CurrentStation.ListenUrl)
			appStatus.StreamPlayer.Play()
			appStatus.PlayStatus = Loading
		} else {
			if appStatus.PlayStatus == Playing {
				appStatus.PlayStatus = Stopped
				playButton.SetIcon(theme.MediaPlayIcon())
				appStatus.StreamPlayer.Stop()
			} else {
				appStatus.PlayStatus = Loading
				playButton.SetText("(Buffering)")
				playButton.SetIcon(theme.MediaStopIcon())
				appStatus.StreamPlayer.Load(appStatus.CurrentStation.ListenUrl)
				appStatus.StreamPlayer.Play()
			}
		}
		volumeBind.Reload()
	})

	volumeContainer := container.NewBorder(
		nil,
		nil,
		volumeDown,
		volumeUp,
		volumeBar,
	)

	playButton.Importance = widget.HighImportance

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
		container.NewCenter(radioSpiralHeaderImage),
		container.NewCenter(widget.NewHyperlink("https://radiospiral.net", rsUrl)),
		container.NewCenter(aboutButton),
		container.NewPadded(stationSelect),
		centerCardContainer,
		volumeContainer,
		controlContainer,
	))

	return playButton, nil
}

// createAboutWindow constructs the UI for the About Us window
func createAboutWindow(aboutWindow fyne.Window) {
	// Header section
	// We repeat this because reusing the same canvas introduces a bug
	radioSpiralHeaderImage := canvas.NewImageFromResource(resourceHeaderPng)
	radioSpiralHeaderImage.SetMinSize(fyne.NewSize(400, 120))
	radioSpiralHeaderImage.FillMode = canvas.ImageFillContain

	// Just hide the window
	aboutWindow.SetCloseIntercept(func() {
		aboutWindow.Hide()
	})

	// Just hide the window
	closeButton := widget.NewButton("Close", func() {
		aboutWindow.Hide()
	})

	credits, err := fetchCredits()

	if err != nil {
		aboutWindow.SetContent(container.NewVBox(
			radioSpiralHeaderImage,
			container.NewCenter(
				closeButton,
			),
		))
		return
	}

	elements := container.NewVBox(radioSpiralHeaderImage)

	for _, credit := range credits.Credits {
		line := widget.NewLabel(credit.Name + " - " + credit.Role)
		elements.Add(container.NewCenter(line))
	}

	elements.Add(closeButton)

	aboutWindow.SetContent(elements)
}
