package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Workflow executors. "comfyui" is reserved for running the same registry
// entries on our own ComfyUI servers.
const (
	MediaWorkflowExecutorRunningHub = "runninghub"
	MediaWorkflowExecutorComfyUI    = "comfyui"
)

// MediaWorkflow is one registered workflow exposed as a gateway model.
//
// ApiJSON / UiJSON / Analysis have no explicit column type on purpose: GORM
// then maps them to LONGTEXT on MySQL (an editor export easily exceeds
// TEXT's 64 KB) and TEXT on PostgreSQL / SQLite.
type MediaWorkflow struct {
	Id             int    `json:"id"`
	ModelName      string `json:"model_name" gorm:"size:128;not null;uniqueIndex"`
	Title          string `json:"title" gorm:"size:255"`
	SourceURL      string `json:"source_url" gorm:"size:512"`
	WorkflowID     string `json:"workflow_id" gorm:"size:64;index"`
	MediaKind      string `json:"media_kind" gorm:"size:16"`
	Executor       string `json:"executor" gorm:"size:32"`
	ApiJSON        string `json:"api_json"`
	UiJSON         string `json:"ui_json,omitempty"`
	Analysis       string `json:"analysis,omitempty"`
	InputMapping   string `json:"input_mapping" gorm:"type:text"`
	OutputNodes    string `json:"output_nodes" gorm:"type:text"`
	PromptTemplate string `json:"prompt_template" gorm:"type:text"`
	Enabled        bool   `json:"enabled"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64  `json:"updated_time" gorm:"bigint"`
}

var ErrMediaWorkflowNotFound = errors.New("media workflow not found")

func (w *MediaWorkflow) Insert() error {
	now := common.GetTimestamp()
	w.CreatedTime = now
	w.UpdatedTime = now
	return DB.Create(w).Error
}

func (w *MediaWorkflow) Update() error {
	w.UpdatedTime = common.GetTimestamp()
	return DB.Save(w).Error
}

func (w *MediaWorkflow) Delete() error {
	return DB.Delete(w).Error
}

// MediaWorkflowSummary is the list view: no JSON bodies.
type MediaWorkflowSummary struct {
	Id          int    `json:"id"`
	ModelName   string `json:"model_name"`
	Title       string `json:"title"`
	SourceURL   string `json:"source_url"`
	WorkflowID  string `json:"workflow_id"`
	MediaKind   string `json:"media_kind"`
	Executor    string `json:"executor"`
	Enabled     bool   `json:"enabled"`
	UpdatedTime int64  `json:"updated_time"`
}

func ListMediaWorkflows() ([]MediaWorkflowSummary, error) {
	items := make([]MediaWorkflowSummary, 0)
	err := DB.Model(&MediaWorkflow{}).
		Select("id", "model_name", "title", "source_url", "workflow_id", "media_kind", "executor", "enabled", "updated_time").
		Order("id desc").Find(&items).Error
	return items, err
}

func GetMediaWorkflowByID(id int) (*MediaWorkflow, error) {
	var workflow MediaWorkflow
	if err := DB.First(&workflow, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMediaWorkflowNotFound
		}
		return nil, err
	}
	return &workflow, nil
}

// GetEnabledMediaWorkflow finds the enabled workflow registered under a model
// name for the given executor.
func GetEnabledMediaWorkflow(modelName, executor string) (*MediaWorkflow, error) {
	var workflow MediaWorkflow
	err := DB.Where("model_name = ? AND executor = ? AND enabled = ?", strings.TrimSpace(modelName), executor, true).
		First(&workflow).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMediaWorkflowNotFound
		}
		return nil, err
	}
	return &workflow, nil
}

func IsMediaWorkflowModelNameTaken(id int, modelName string) (bool, error) {
	var count int64
	err := DB.Model(&MediaWorkflow{}).Where("model_name = ? AND id <> ?", strings.TrimSpace(modelName), id).Count(&count).Error
	return count > 0, err
}
