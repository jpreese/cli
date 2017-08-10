package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/exercism/cli/api"
	"github.com/exercism/cli/config"
	"github.com/exercism/cli/workspace"
	"github.com/spf13/cobra"
)

// downloadCmd lets people download exercises and associated solutions.
var downloadCmd = &cobra.Command{
	Use:     "download",
	Aliases: []string{"d"},
	Short:   "Download an exercise.",
	Long: `Download an exercise.

You may download an exercise to work on. If you've already
started working on it, the command will also download your
latest solution.

Download other people's solutions by providing the UUID.
`,
	Run: func(cmd *cobra.Command, args []string) {
		uuid, err := cmd.Flags().GetString("uuid")
		BailOnError(err)
		if uuid == "" && len(args) == 0 {
			// TODO: usage
			fmt.Fprintf(os.Stderr, "need an exercise name or a solution --uuid")
			return
		}

		var exercise string
		if len(args) > 0 {
			exercise = args[0]
		}

		track, err := cmd.Flags().GetString("track")
		BailOnError(err)

		dr, err := newDownloadRequest(uuid, track, exercise)
		BailOnError(err)

		res, err := dr.do()
		BailOnError(err)

		if res.StatusCode != http.StatusOK {
			switch dr.payload.Error.Type {
			case "track_ambiguous":
				// TODO: interactive selection
				fmt.Fprintf(os.Stderr, "You have multiple %s exercises available to you.\n", solution.Exercise)
				fmt.Fprintln(os.Stderr, "Specify the the --track flag:")
				for _, id := range dr.payload.Error.PossibleTrackIDs {
					fmt.Fprintf(os.Stderr, "%s download %s --track=%s", BinName, solution.Exercise, id)
				}
				os.Exit(1)
			default:
				BailOrError(errors.New(dr.payload.Error.Message))
			}
		}

		solution := dr.newSolution()

		parent := filepath.Join(dr.client.UserConfig.Workspace, solution.PathToParent())
		os.MkdirAll(parent, os.FileMode(0755))

		ws := workspace.New(parent)
		dir, err := ws.SolutionPath(solution.Exercise, solution.ID)
		BailOnError(err)

		os.MkdirAll(dir, os.FileMode(0755))

		err = solution.Write(dir)
		BailOnError(err)

		for _, file := range dr.payload.Solution.Files {
			url := fmt.Sprintf("%s%s", dr.payload.Solution.FileDownloadBaseURL, file)
			req, err := dr.client.NewRequest("GET", url, nil)
			BailOnError(err)

			res, err := dr.client.Do(req, nil)
			BailOnError(err)
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				BailOnError(fmt.Errorf("Failed to download: %s", file))
				continue
			}

			// Don't bother with empty files.
			if res.Header.Get("Content-Length") == "0" {
				continue
			}

			// TODO: if there's a collision, interactively resolve (show diff, ask if overwrite).
			// TODO: handle --force flag to overwrite without asking.
			relativePath := filepath.FromSlash(file)
			dir := filepath.Join(solution.Dir, filepath.Dir(relativePath))
			os.MkdirAll(dir, os.FileMode(0755))

			f, err := os.Create(filepath.Join(solution.Dir, relativePath))
			BailOnError(err)
			defer f.Close()
			_, err = io.Copy(f, res.Body)
			BailOnError(err)
		}
		fmt.Fprintf(Out, "\nDownloaded to\n%s\n", solution.Dir)
	},
}

type downloadRequest struct {
	client  *api.Client
	request *http.Request
	payload downloadPayload
}

func newDownloadRequest(uuid, track, exercise string) (downloadRequest, error) {
	cfg, err := config.NewAPIConfig()
	if err != nil {
		return downloadRequest{}, err
	}

	var slug string
	if uuid == "" {
		slug = "latest"
	} else {
		slug = uuid
	}
	url := cfg.URL("download", slug)

	client, err := api.NewClient()
	if err != nil {
		return downloadRequest{}, err
	}

	req, err := client.NewRequest("GET", url, nil)
	if err != nil {
		return downloadRequest{}, err
	}

	if uuid == "" {
		q := req.URL.Query()
		q.Add("exercise_id", exercise)
		if track != "" {
			q.Add("track_id", track)
		}
		req.URL.RawQuery = q.Encode()
	}

	dReq := downloadRequest{
		client:  client,
		request: req,
		payload: downloadPayload{},
	}
	return dReq, nil
}

func (dr downloadRequest) newSolution() *workspace.Solution {
	s := dr.payload.Solution
	return &workspace.Solution{
		Track:       s.Exercise.Track.ID,
		Exercise:    s.Exercise.ID,
		ID:          s.ID,
		URL:         s.URL,
		Handle:      s.User.Handle,
		IsRequester: s.User.IsRequester,
	}
}

func (dr downloadRequest) do() (*http.Response, error) {
	return dr.client.Do(dr.request, &dr.payload)
}

type downloadPayload struct {
	Solution struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		User struct {
			Handle      string `json:"handle"`
			IsRequester bool   `json:"is_requester"`
		} `json:"user"`
		Exercise struct {
			ID              string `json:"id"`
			InstructionsURL string `json:"instructions_url"`
			AutoApprove     bool   `json:"auto_approve"`
			Track           struct {
				ID       string `json:"id"`
				Language string `json:"language"`
			} `json:"track"`
		} `json:"exercise"`
		FileDownloadBaseURL string   `json:"file_download_base_url"`
		Files               []string `json:"files"`
		Iteration           struct {
			SubmittedAt *string `json:"submitted_at"`
		}
	} `json:"solution"`
	Error struct {
		Type             string   `json:"type"`
		Message          string   `json:"message"`
		PossibleTrackIDs []string `json:"possible_track_ids"`
	} `json:"error,omitempty"`
}

func initDownloadCmd() {
	downloadCmd.Flags().StringP("uuid", "u", "", "the solution UUID")
	downloadCmd.Flags().StringP("track", "t", "", "the track ID")
}

func init() {
	RootCmd.AddCommand(downloadCmd)
	initDownloadCmd()
}
