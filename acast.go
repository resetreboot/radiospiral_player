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
 * This module deals with calling AzuraCast APIs to get info, update stuff
 * Probably would be better to have an AC API loaded but we're just doing
 * very simple stuff to import a whole package for things we will never use.
 */

import (
	"encoding/json"
	"image"
	"io"
	"log"
	"net/http"
	"strings"
)

// Main RadioSpiral
const STATIONS_QUERY_URL = "https://spiral.radio/api/stations"
const NOWPLAYING_URL = "https://radiospiral.radio/api/nowplaying/"

const REMOVE_TEST_STATION = "rstest"

type StationInfo struct {
	Id              int    `json:"id"`
	Name            string `json:"name"`
	Shortcode       string `json:"shortcode"`
	Description     string `json:"description"`
	Frontend        string `json:"frontend"`
	Backend         string `json:"backend"`
	Timezone        string `json:"timezone"`
	ListenUrl       string `json:"listen_url"`
	Url             string `json:"url"`
	PublicPlayerUrl string `json:"public_player_url"`
	PlaylistPlsUrl  string `json:"playlist_pls_url"`
	PlaylistM3uUrl  string `json:"playlist_m3u_url"`
	IsPublic        bool   `json:"is_public"`
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
func queryStation(station StationInfo) (*StationResponse, error) {
	apiEndpoint := NOWPLAYING_URL + station.Shortcode
	resp, err := http.Get(apiEndpoint)
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

// Query the stations available
func fetchStations() ([]StationInfo, error) {
	resp, err := http.Get(STATIONS_QUERY_URL)
	if err != nil {
		// If we get an error fetching the data, await a minute and retry
		log.Println("[ERROR] Error when querying available stations")
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

	var response []StationInfo
	json.Unmarshal(body, &response)

	stations := make([]StationInfo, 0)
	for _, elem := range response {
		// We filter the test station
		if elem.Shortcode != REMOVE_TEST_STATION {
			stations = append(stations, elem)
		}
	}

	return stations, nil
}
