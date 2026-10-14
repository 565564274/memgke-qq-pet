package server

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"qq-pet/backend/internal/auth"
	"qq-pet/backend/internal/manualpk"
	"qq-pet/backend/internal/response"
)

func manualPKHandler(db *sql.DB,scheduler *manualpk.Scheduler,action string)http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){
	user,ok:=auth.UserFromContext(r.Context());if !ok{response.JSON(w,http.StatusUnauthorized,1001,"请先登录",nil);return}
	binding,err:=userBindingID(r,db,user.ID);if err!=nil{writeAutoControlBindingError(w,err);return};if scheduler==nil{response.JSON(w,http.StatusServiceUnavailable,5031,"手动 PK 调度器未启动",nil);return}
	switch action{case "start":var input struct{TargetStarts int64 `json:"target_starts"`};if json.NewDecoder(r.Body).Decode(&input)!=nil{response.JSON(w,http.StatusBadRequest,4003,"目标次数不合法",nil);return};if err=scheduler.StartBinding(binding,r.PathValue("qq"),input.TargetStarts);err!=nil{response.JSON(w,http.StatusBadRequest,4003,err.Error(),nil);return};case "stop":err=scheduler.StopBinding(binding);if err!=nil{response.JSON(w,http.StatusInternalServerError,9002,"手动 PK 停止失败",nil);return}}
	state,err:=scheduler.State(r.Context(),binding);if err!=nil{response.JSON(w,http.StatusInternalServerError,9002,"手动 PK 状态读取失败",nil);return};response.JSON(w,http.StatusOK,0,"ok",state)
}}
