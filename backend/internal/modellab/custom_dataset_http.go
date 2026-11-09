package modellab

import (
	"errors"
	"net/http"

	"github.com/noosphere/backend/internal/modelhttp"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func (s *Service) registerDatasetRoutes(route func(string, func(http.ResponseWriter, *http.Request) error)) {
	route("GET /api/workspaces/{ws}/datasets", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r, "include_archived"); err != nil {
			return err
		}
		value := r.URL.Query().Get("include_archived")
		if value != "" && value != "true" && value != "false" {
			return workspaces.ErrInvalid
		}
		datasets, err := s.ListDatasets(r.Context(), r.PathValue("ws"), value == "true")
		if err != nil {
			return err
		}
		modelhttp.JSON(w, http.StatusOK, map[string]any{"datasets": datasets})
		return nil
	})
	route("POST /api/workspaces/{ws}/datasets", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var body DatasetDocument
		if err := modelhttp.Decode(w, r, &body); err != nil {
			return err
		}
		detail, err := s.SaveDataset(r.Context(), r.PathValue("ws"), body)
		if errors.Is(err, modelstore.ErrInvalid) {
			modelhttp.Error(w, workspaces.ErrInvalid, map[string]any{"validation": err.Error()})
			return nil
		}
		if err != nil {
			return err
		}
		modelhttp.JSON(w, http.StatusCreated, detail)
		return nil
	})
	route("GET /api/workspaces/{ws}/datasets/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		detail, err := s.GetDataset(r.Context(), r.PathValue("ws"), r.PathValue("id"))
		if err != nil {
			return err
		}
		modelhttp.JSON(w, http.StatusOK, detail)
		return nil
	})
	route("POST /api/workspaces/{ws}/datasets/{id}/archive", func(w http.ResponseWriter, r *http.Request) error {
		if err := modelhttp.Query(r); err != nil {
			return err
		}
		var body struct {
			Archived *bool `json:"archived"`
		}
		if err := modelhttp.Decode(w, r, &body); err != nil {
			return err
		}
		if body.Archived == nil {
			return workspaces.ErrInvalid
		}
		revision, err := s.ArchiveDataset(r.Context(), r.PathValue("ws"), r.PathValue("id"), *body.Archived)
		if err != nil {
			return err
		}
		modelhttp.JSON(w, http.StatusOK, map[string]any{"revision": revision})
		return nil
	})
}
