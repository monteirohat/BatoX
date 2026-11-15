package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/video"
)

type videoResponse struct {
	video.State
	// Warning explains a partial answer, such as screens read from the
	// kernel because Batocera couldn't list them.
	Warning string `json:"warning,omitempty"`
}

// videoState: GET /video -> the screens, the saved choice, the screens the
// next start will use, and whether a restart is needed to apply it.
func (s *Server) videoState(w http.ResponseWriter, _ *http.Request) {
	resp, _, err := s.readVideo()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// setVideo: PUT /video {"main": "HDMI-A-2", "second": "none"} -> saves the
// choice in batocera.conf. A missing field keeps its saved value. "main" is
// "auto" or a screen; "second" is "auto", "none" (off) or a screen. It takes
// effect when the console restarts.
func (s *Server) setVideo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Main   *string `json:"main"`
		Second *string `json:"second"`
	}
	if err := decodeJSON(r, &body); err != nil || (body.Main == nil && body.Second == nil) {
		writeError(w, http.StatusBadRequest, `expected body: {"main": "auto", "second": "auto"}`)
		return
	}
	cur, listed, err := s.readVideo()
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !listed {
		// Names read from the kernel may not be the ones Batocera expects
		// (under X11 they differ), so nothing is saved without its list.
		writeError(w, http.StatusServiceUnavailable, "couldn't list the screens, so nothing was saved: "+cur.Warning)
		return
	}
	next := cur.Settings
	if body.Main != nil {
		next.Main = *body.Main
	}
	if body.Second != nil {
		next.Second = *body.Second
	}
	next = video.FromConf(next.Main, next.Second)

	names := make([]string, len(cur.Outputs))
	for i, o := range cur.Outputs {
		names[i] = o.Name
	}
	if err := next.Validate(names); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	main, second := next.ConfValues()
	if err := s.Platform.SetSetting(video.KeyMain, main); err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.Platform.SetSetting(video.KeySecond, second); err != nil {
		s.internalError(w, err)
		return
	}
	s.Log.Infof("video: main screen %s, second screen %s (applies at the next restart)", next.Main, next.Second)

	resp, _, err := s.readVideo()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// readVideo gathers the video state. listed is false when Batocera couldn't
// list its screens and the names come from the kernel instead.
func (s *Server) readVideo() (resp videoResponse, listed bool, err error) {
	displays := s.Video.Displays()
	names, listErr := s.Platform.VideoOutputs()
	listed = listErr == nil
	if !listed {
		s.Log.Errorf("video: %v", listErr)
		for _, d := range displays {
			if d.Connected {
				names = append(names, d.Name)
			}
		}
		resp.Warning = fmt.Sprintf("Batocera couldn't list its screens (%v).", listErr)
	}
	main, err := s.optionalSetting(video.KeyMain)
	if err != nil {
		return resp, listed, err
	}
	second, err := s.optionalSetting(video.KeySecond)
	if err != nil {
		return resp, listed, err
	}
	resp.State = video.Describe(names, displays, s.Video.CurrentMain(), video.FromConf(main, second))
	return resp, listed, nil
}

// optionalSetting reads a batocera.conf key; a missing key is "".
func (s *Server) optionalSetting(key string) (string, error) {
	v, err := s.Platform.GetSetting(key)
	if errors.Is(err, platform.ErrSettingNotFound) {
		return "", nil
	}
	return v, err
}
