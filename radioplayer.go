package main

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
