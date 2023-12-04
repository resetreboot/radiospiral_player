package main

import (
	"bufio"
	"io"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// helper
func check(err error) {
	if err != nil {
		panic(err)
	}
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
			player.command = exec.Command(player.player_name, "-quiet", "-playlist", stream_url)
		} else {
			player.command = exec.Command(player.player_name, "-quiet", stream_url)
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

func main() {
	status_chan := make(chan string)
	pipe_chan := make(chan io.ReadCloser)

	mplayer := MPlayer{player_name: "mplayer", is_playing: false, pipe_chan: pipe_chan}

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

	app := app.New()
	window := app.NewWindow("RadioSpiral")

	radiospiral_label := widget.NewLabel("RadioSpiral")
	nowplaying_label := widget.NewLabel("Not Playing")

	window.SetContent(container.NewVBox(
		radiospiral_label,
		nowplaying_label,
		widget.NewButton("Play", func() {
			if !mplayer.is_playing {
				nowplaying_label.SetText("Playing!")
				mplayer.Play("http://radiospiral.radio/stream.mp3")
			} else {
				nowplaying_label.SetText("Stopped")
				mplayer.Pause()
			}
		}),
	))

	window.ShowAndRun()
	mplayer.Close()
}
