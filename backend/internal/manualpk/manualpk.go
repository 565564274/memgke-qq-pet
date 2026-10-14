package manualpk

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"qq-pet/backend/internal/timeutil"
)

type Client interface {
	GetPetPKStrangers(context.Context, int64, string, int64) (json.RawMessage, error)
	StartPetPK(context.Context, int64, string, string, string) (json.RawMessage, error)
	GetPetPKStatus(context.Context, int64, string, string) (json.RawMessage, error)
	SettlePetPK(context.Context, int64, string, string) (json.RawMessage, error)
}

type State struct {
	Enabled bool `json:"enabled"`
	TargetStarts int64 `json:"target_starts"`
	TodayStarts int64 `json:"today_starts"`
	TotalStarts int64 `json:"total_starts"`
	TodayDate string `json:"today_date"`
	Rounds int64 `json:"rounds"`
	Message string `json:"message"`
	UpdatedAt string `json:"updated_at"`
	Cursor string `json:"-"`
	StoryID string `json:"-"`
	CurrentUserID string `json:"-"`
	CurrentPetID string `json:"-"`
	CurrentAttempt int64 `json:"-"`
	Logs []Log `json:"logs"`
}

type Log struct { Message string `json:"message"`; CreatedAt string `json:"created_at"` }

type Scheduler struct { db *sql.DB; client Client; mu sync.Mutex; ctx context.Context; cancel context.CancelFunc; workers map[int64]context.CancelFunc }

func NewScheduler(db *sql.DB, client Client) *Scheduler { return &Scheduler{db:db, client:client, workers:map[int64]context.CancelFunc{}} }

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock(); if s.cancel != nil { s.mu.Unlock(); return }; root,cancel:=context.WithCancel(ctx); s.ctx=root; s.cancel=cancel; s.mu.Unlock()
	rows,err:=s.db.QueryContext(root,"SELECT s.qq_binding_id,s.target_starts,q.qq_number FROM pet_manual_pk_states s JOIN qq_bindings q ON q.id=s.qq_binding_id WHERE s.enabled=1"); if err!=nil{return}; defer rows.Close()
	type binding struct{id,target int64; qq string};ids:=[]binding{}; for rows.Next(){var b binding;if rows.Scan(&b.id,&b.target,&b.qq)==nil{ids=append(ids,b)}}
	for _,b:=range ids {_=s.StartBinding(b.id,b.qq,b.target)}
}

func (s *Scheduler) Stop(){s.mu.Lock(); if s.cancel!=nil{s.cancel();s.cancel=nil;s.ctx=nil}; stops:=[]context.CancelFunc{};for id,c:=range s.workers{stops=append(stops,c);delete(s.workers,id)};s.mu.Unlock();for _,stop:=range stops{stop()} }

func (s *Scheduler) StartBinding(bindingID int64,qq string,target int64) error {
	if target<1{return errors.New("目标 PK 次数须大于 0")}
	now:=timeutil.Now();day:=timeutil.Current().Format("2006-01-02")
	_,err:=s.db.Exec(`INSERT INTO pet_manual_pk_states(qq_binding_id,enabled,target_starts,today_date,message,updated_at) VALUES(?,1,?,?,?,?) ON CONFLICT(qq_binding_id) DO UPDATE SET enabled=1,target_starts=excluded.target_starts,message='手动 PK 运行中',updated_at=excluded.updated_at`,bindingID,target,day,"手动 PK 运行中",now);if err!=nil{return err}
	s.mu.Lock(); if _,ok:=s.workers[bindingID];ok{s.mu.Unlock();return nil};if s.ctx==nil{s.ctx,s.cancel=context.WithCancel(context.Background())};ctx,cancel:=context.WithCancel(s.ctx);s.workers[bindingID]=cancel;s.mu.Unlock()
	go func(){defer func(){s.mu.Lock();delete(s.workers,bindingID);s.mu.Unlock()}();s.run(ctx,bindingID,qq)}();return nil
}

func(s *Scheduler) StopBinding(bindingID int64) error { _,err:=s.db.Exec("UPDATE pet_manual_pk_states SET enabled=0,message='手动 PK 已停止',updated_at=? WHERE qq_binding_id=?",timeutil.Now(),bindingID);s.mu.Lock();c:=s.workers[bindingID];delete(s.workers,bindingID);s.mu.Unlock();if c!=nil{c()};return err }

func(s *Scheduler) State(ctx context.Context,bindingID int64)(State,error){var v State;var enabled int;err:=s.db.QueryRowContext(ctx,"SELECT enabled,target_starts,today_starts,total_starts,today_date,rounds,message,updated_at,cursor,story_id,current_user_id,current_pet_id,current_attempt FROM pet_manual_pk_states WHERE qq_binding_id=?",bindingID).Scan(&enabled,&v.TargetStarts,&v.TodayStarts,&v.TotalStarts,&v.TodayDate,&v.Rounds,&v.Message,&v.UpdatedAt,&v.Cursor,&v.StoryID,&v.CurrentUserID,&v.CurrentPetID,&v.CurrentAttempt);v.Enabled=enabled==1;if errors.Is(err,sql.ErrNoRows){return State{Message:"手动 PK 未启动",TodayDate:timeutil.Current().Format("2006-01-02"),Logs:[]Log{}},nil};if err!=nil{return v,err};rows,err:=s.db.QueryContext(ctx,"SELECT message,created_at FROM pet_manual_pk_logs WHERE qq_binding_id=? ORDER BY id DESC LIMIT 100",bindingID);if err!=nil{return v,err};defer rows.Close();v.Logs=[]Log{};for rows.Next(){var l Log;if err:=rows.Scan(&l.Message,&l.CreatedAt);err!=nil{return v,err};v.Logs=append(v.Logs,l)};return v,rows.Err()}

func(s *Scheduler) run(ctx context.Context,bindingID int64,qq string){
	var cursor,opponent,story string; var opponentPet string; var tries,miss int64
	if initial,err:=s.State(ctx,bindingID);err==nil{cursor=initial.Cursor;story=initial.StoryID;opponent=initial.CurrentUserID;opponentPet=initial.CurrentPetID;tries=initial.CurrentAttempt}
	for { select{case <-ctx.Done():return;default:}
		st,err:=s.State(ctx,bindingID);if err!=nil||!st.Enabled{return};story=st.StoryID;opponent=st.CurrentUserID;opponentPet=st.CurrentPetID;tries=st.CurrentAttempt;today:=timeutil.Current().Format("2006-01-02");if st.TodayDate!=today{_,_=s.db.Exec("UPDATE pet_manual_pk_states SET today_starts=0,today_date=?,rounds=0,updated_at=? WHERE qq_binding_id=?",today,timeutil.Now(),bindingID);st.TodayStarts=0;st.Rounds=0}
		if st.TodayStarts>=st.TargetStarts&&story==""{_,_=s.db.Exec("UPDATE pet_manual_pk_states SET enabled=0,message='目标 PK 次数已完成',updated_at=? WHERE qq_binding_id=?",timeutil.Now(),bindingID);return}
		selfID,_:=strconv.ParseInt(qq,10,64);var selfPet string;if s.db.QueryRowContext(ctx,"SELECT pet_id FROM pet_profiles WHERE qq_binding_id=?",bindingID).Scan(&selfPet)!=nil||selfPet==""{s.log(bindingID,"自身宠物资料不完整");_,_=s.db.Exec("UPDATE pet_manual_pk_states SET enabled=0,message='自身宠物资料不完整，已停止',updated_at=? WHERE qq_binding_id=?",timeutil.Now(),bindingID);return}
		if story!=""{status,e:=s.client.GetPetPKStatus(ctx,selfID,selfPet,story);if e==nil&&responseEmpty(status){_,e=s.client.SettlePetPK(ctx,selfID,selfPet,story);if e==nil{story="";tries++;if tries>=3{opponent="";opponentPet="";tries=0};_,_=s.db.Exec("UPDATE pet_manual_pk_states SET story_id='',current_user_id=?,current_pet_id=?,current_attempt=?,updated_at=? WHERE qq_binding_id=?",opponent,opponentPet,tries,timeutil.Now(),bindingID)}};if e!=nil{s.log(bindingID,e.Error())};if !pause(ctx,12*time.Second){return};continue}
		if opponent!=""&&tries<3 {if begin(ctx,s,selfID,selfPet,opponent,opponentPet,&story,bindingID,st){if !pause(ctx,12*time.Second){return};continue};tries=3}
		if opponent!="" {opponent="";opponentPet="";tries=0}
		var uid,pid string; e:=s.db.QueryRowContext(ctx,`SELECT user_id,pet_id FROM pet_pk_strangers WHERE pet_id<>'' AND user_id NOT IN (SELECT opponent_user_id FROM pet_manual_pk_daily_opponents WHERE qq_binding_id=? AND run_date=? AND start_count>=3) ORDER BY id LIMIT 1`,bindingID,today).Scan(&uid,&pid)
		if errors.Is(e,sql.ErrNoRows){raw,e2:=s.client.GetPetPKStrangers(ctx,selfID,cursor,4);if e2==nil{_,next,n:=insertPage(ctx,s.db,raw);if n==0{miss++}else{miss=0};if next!=""&&next!=cursor{cursor=next};_,_=s.db.Exec("UPDATE pet_manual_pk_states SET cursor=?,rounds=rounds+1,updated_at=? WHERE qq_binding_id=?",cursor,timeutil.Now(),bindingID)}else{miss++};if miss>=10{_,_=s.db.Exec("UPDATE pet_manual_pk_states SET enabled=0,message='连续 10 轮未找到可 PK 对手',updated_at=? WHERE qq_binding_id=?",timeutil.Now(),bindingID);return};if !pause(ctx,3*time.Second){return};continue};if e!=nil{if !pause(ctx,5*time.Second){return};continue}
		opponent,opponentPet,tries=uid,pid,0;if begin(ctx,s,selfID,selfPet,uid,pid,&story,bindingID,st){if !pause(ctx,12*time.Second){return};continue};opponent="";miss++;if !pause(ctx,3*time.Second){return}
	}
}

func begin(ctx context.Context,s *Scheduler,selfID int64,selfPet,uid,pid string,story *string,binding int64,st State)bool{raw,err:=s.client.StartPetPK(ctx,selfID,selfPet,uid,pid);if err!=nil{return false};sid:=findString(raw,"story_id");if sid==""{return false};*story=sid;today:=timeutil.Current().Format("2006-01-02");now:=timeutil.Now();_,_=s.db.Exec(`INSERT INTO pet_manual_pk_daily_opponents(qq_binding_id,opponent_user_id,run_date,start_count,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(qq_binding_id,opponent_user_id,run_date) DO UPDATE SET start_count=start_count+1,updated_at=excluded.updated_at`,binding,uid,today,now);_,_=s.db.Exec("UPDATE pet_manual_pk_states SET today_starts=today_starts+1,total_starts=total_starts+1,message='PK 进行中',story_id=?,current_user_id=?,current_pet_id=?,current_attempt=CASE WHEN current_user_id=? THEN current_attempt+1 ELSE 1 END,updated_at=? WHERE qq_binding_id=?",sid,uid,pid,uid,now,binding);s.log(binding,"PK 已开始: "+uid);return true}

func insertPage(ctx context.Context,db *sql.DB,raw json.RawMessage)(string,string,int){var root map[string]any;_ = json.Unmarshal(raw,&root);data,_:=root["data"].(map[string]any);if data==nil{data=root};arr,_:=data["friends"].([]any);n:=0;for _,v:=range arr{m,ok:=v.(map[string]any);if !ok{continue};uid:=stringValue(m["user_id"]);pid:=stringValue(m["pet_id"]);if uid==""||pid==""{continue};b,_:=json.Marshal(m);_,_=db.ExecContext(ctx,`INSERT INTO pet_pk_strangers(user_id,pet_id,pet_name,nickname,raw_data,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET pet_id=excluded.pet_id,raw_data=excluded.raw_data,updated_at=excluded.updated_at`,uid,pid,stringValue(m["pet_name"]),stringValue(m["nickname"]),string(b),timeutil.Now());n++};return stringValue(data["cursor"]),stringValue(data["next_cursor"]),n}

func stringValue(value any)string{if value==nil{return ""};return strings.TrimSpace(fmt.Sprint(value))}
func findString(raw []byte,key string)string{var walk func(any)string;walk=func(v any)string{switch x:=v.(type){case map[string]any:if text,ok:=x[key].(string);ok&&strings.TrimSpace(text)!=""{return strings.TrimSpace(text)};for _,child:=range x{if found:=walk(child);found!=""{return found}};case []any:for _,child:=range x{if found:=walk(child);found!=""{return found}}};return ""};var root any;_ = json.Unmarshal(raw,&root);return walk(root)}
func responseEmpty(raw []byte)bool{var walk func(any)bool;walk=func(v any)bool{switch x:=v.(type){case map[string]any:if x["response_empty"]==true{return true};for _,child:=range x{if walk(child){return true}};case []any:for _,child:=range x{if walk(child){return true}}};return false};var root any;if len(strings.TrimSpace(string(raw)))==0{return true};_ = json.Unmarshal(raw,&root);return walk(root)}
func(s *Scheduler)log(binding int64,msg string){_,_=s.db.Exec("INSERT INTO pet_manual_pk_logs(qq_binding_id,message,created_at) VALUES(?,?,?)",binding,msg,timeutil.Now())}
func pause(ctx context.Context,d time.Duration)bool{t:=time.NewTimer(d);defer t.Stop();select{case <-ctx.Done():return false;case <-t.C:return true}}
