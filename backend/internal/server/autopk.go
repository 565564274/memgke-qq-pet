package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/autopk"
	"qq-pet/backend/internal/response"
)

func autoPKConfigHandler(db *sql.DB, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		if !update {
			config, err := autopk.LoadConfig(r.Context(), db, bindingID)
			if err != nil {
				response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 配置读取失败", nil)
				return
			}
			config.TargetStarts = 10
			config.StartTime = "00:00"
			response.JSON(w, http.StatusOK, 0, "ok", config)
			return
		}
		var input struct {
			TargetStarts int64  `json:"target_starts"`
			StartTime    string `json:"start_time"`
			PreferredFriends []autopk.PreferredFriend `json:"preferred_friends"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			response.JSON(w, http.StatusBadRequest, 4003, "自动 PK 配置不合法", nil)
			return
		}
		seenFriends := make(map[string]bool, len(input.PreferredFriends))
		for _, friend := range input.PreferredFriends {
			if strings.TrimSpace(friend.UserID) == "" || strings.TrimSpace(friend.PetID) == "" || seenFriends[strings.TrimSpace(friend.UserID)] {
				response.JSON(w, http.StatusBadRequest, 4003, "优先 PK 好友配置不合法", nil)
				return
			}
			seenFriends[strings.TrimSpace(friend.UserID)] = true
		}
		config, err := autopk.LoadConfig(r.Context(), db, bindingID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 配置读取失败", nil)
			return
		}
		// 自动 PK 的次数和开始时间由系统固定，客户端只负责保存兼容请求。
		config.TargetStarts = 10
		config.StartTime = "00:00"
		config.PreferredFriends = input.PreferredFriends
		if err := autopk.SaveConfig(r.Context(), db, bindingID, config); err != nil {
			response.JSON(w, http.StatusBadRequest, 4003, err.Error(), nil)
			return
		}
		config, _ = autopk.LoadConfig(r.Context(), db, bindingID)
		response.JSON(w, http.StatusOK, 0, "自动 PK 配置已保存", config)
	}
}

func autoPKStartHandler(db *sql.DB, scheduler *autopk.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		if scheduler == nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "自动 PK 调度器未启动", nil)
			return
		}
		// 自动 PK 的执行策略由系统统一固定，用户端只控制启停。
		config, loadErr := autopk.LoadConfig(r.Context(), db, bindingID)
		if loadErr != nil { response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 配置读取失败", nil); return }
		config.Enabled, config.TargetStarts, config.StartTime = true, 10, "00:00"
		if err := autopk.SaveConfig(r.Context(), db, bindingID, config); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 配置保存失败", nil)
			return
		}
		if err := scheduler.Enable(context.Background(), bindingID, strings.TrimSpace(r.PathValue("qq"))); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 启动失败", nil)
			return
		}
		state, _ := autopk.LoadState(r.Context(), db, bindingID)
		response.JSON(w, http.StatusOK, 0, "自动 PK 已启动", state)
	}
}

func autoPKStopHandler(db *sql.DB, scheduler *autopk.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		if scheduler == nil {
			response.JSON(w, http.StatusServiceUnavailable, 5031, "自动 PK 调度器未启动", nil)
			return
		}
		if err := scheduler.Disable(r.Context(), bindingID, "自动 PK 已停止"); err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 停止失败", nil)
			return
		}
		state, _ := autopk.LoadState(r.Context(), db, bindingID)
		response.JSON(w, http.StatusOK, 0, "自动 PK 已停止", state)
	}
}

func autoPKStateHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		state, err := autopk.LoadState(r.Context(), db, bindingID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 状态读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", state)
	}
}

func autoPKLogsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, 1001, "请先登录", nil)
			return
		}
		bindingID, err := userBindingID(r, db, user.ID)
		if err != nil {
			writeAutoControlBindingError(w, err)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		date := strings.TrimSpace(r.URL.Query().Get("date"))
		if date != "" && (len(date) != 10 || date[4] != '-' || date[7] != '-') { response.JSON(w, http.StatusBadRequest, 4003, "日期格式须为 YYYY-MM-DD", nil); return }
		logs, err := autopk.LoadLogsByDate(r.Context(), db, bindingID, limit, date)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, 9002, "自动 PK 日志读取失败", nil)
			return
		}
		response.JSON(w, http.StatusOK, 0, "ok", logs)
	}
}
