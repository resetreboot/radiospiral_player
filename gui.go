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

import (
	"log"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

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
