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
 * Small interface and object to grab ffmpeg and start streaming, piping the
 * raw wave output to Oto to send the audio back to the OS audio system
 *
 */

import (
	"io"
	"log"
	"os/exec"
	"strings"

	"github.com/ebitengine/oto/v3"
)

// Radio player interface
type RadioPlayer interface {
	Load(stream_url string)
	IsPlaying() bool
	IsMuted() bool
	Play()
	Mute()
	Stop()
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
	otoContext    *oto.Context
	otoPlayer     *oto.Player
	currentVolume float64
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
		player.out = nil

		player.stream_url = ""
	}
}

func (player *StreamPlayer) IsMuted() bool {
	return player.otoPlayer.Volume() == 0.0
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

func (player *StreamPlayer) Stop() {
	if player.IsPlaying() {
		player.Close()
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
