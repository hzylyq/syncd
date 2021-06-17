package deploy

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dreamans/syncd"
	"github.com/dreamans/syncd/module/deploy"
	"github.com/dreamans/syncd/module/project"
	"github.com/dreamans/syncd/module/server"
	"github.com/dreamans/syncd/module/user"
	"github.com/dreamans/syncd/util/command"
)

var statusMap = map[int]string{
	1: "部署成功",
	0: "部署失败",
}

type deployMessage struct {
	ApplyId int
	Mode    int
	Status  int
	Title   string
}

type FeiShuReq struct {
	MsgType string `json:"msg_type"`
	Content struct {
		Post struct {
			ZhCn struct {
				Title   string      `json:"title"`
				Content [][]content `json:"content"`
			} `json:"zh_cn"`
		} `json:"post"`
	} `json:"content"`
	Timestamp string `json:"timestamp"`
	Sign      string `json:"sign"`
}

func applyLink(applyId int) string {
	return fmt.Sprintf("%s/deploy/deploy?id=%d", syncd.App.AppHost, applyId)
}

func SendToFeishu(msg *deployMessage) {
	go sendToFeishu(msg)
}

func sendToFeishu(msg *deployMessage) {
	url := syncd.App.Feishu.Url

	apply := &deploy.Apply{
		ID: msg.ApplyId,
	}
	if err := apply.Detail(); err != nil {
		return
	}
	project := &project.Project{
		ID: apply.ProjectId,
	}
	if err := project.Detail(); err != nil {
		return
	}

	groupMap, err := server.GroupGetMapByIds(project.OnlineCluster)
	if err != nil {
		log.Print(err)
		return
	}
	var groupName []string
	for _, group := range groupMap {
		groupName = append(groupName, group.Name)
	}

	serverList, err := server.ServerGetListByGroupIds(project.OnlineCluster)
	if err != nil {
		return
	}

	u := &user.User{
		ID: apply.UserId,
	}

	if err := u.Detail(); err != nil {
		return
	}

	conf := &deployMessageConf{
		msg:        msg,
		apply:      apply,
		project:    project,
		serverNums: len(serverList),
		userName:   u.Username,
		groupName:  strings.Join(groupName, ","),
		commitMsg:  command.CommitMsg(),
	}

	req, err := conf.NewMessage()
	if err != nil {
		return
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return
	}

	request, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")

	client := http.DefaultClient
	resp, err := client.Do(request)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
}

func GenSign(secret string, timestamp int64) (string, error) {
	// timestamp + key 做sha256, 再进行base64 encode
	stringToSign := fmt.Sprintf("%v", timestamp) + "\n" + secret
	var data []byte
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, err := h.Write(data)
	if err != nil {
		return "", err
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return signature, nil
}

type deployMessageConf struct {
	msg        *deployMessage
	apply      *deploy.Apply
	project    *project.Project
	serverNums int
	userName   string
	groupName  string
	commitMsg  string
}

type content struct {
	Tag  string `json:"tag"`
	Text string `json:"text,omitempty"`
	Href string `json:"href,omitempty"`
}

type SendFeishuReq struct {
	Email   string `json:"email"`
	MsgType string `json:"msg_type"`
	Content struct {
		Post struct {
			ZhCn struct {
				Title   string      `json:"title"`
				Content [][]content `json:"content"`
			} `json:"zh_cn"`
		} `json:"post"`
	} `json:"content"`
	Timestamp string `json:"timestamp"`
	Sign      string `json:"sign"`
}

func (conf *deployMessageConf) NewMessage() (*FeiShuReq, error) {
	var req FeiShuReq
	req.MsgType = "post"
	req.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	req.Sign, _ = GenSign(syncd.App.Feishu.Sign, time.Now().Unix())
	req.Content.Post.ZhCn.Title = statusMap[conf.msg.Status]
	req.Content.Post.ZhCn.Content = make([][]content, 0)

	res := make([][]content, 7)
	for i := 0; i < 7; i++ {
		if i == 5 {
			res[i] = make([]content, 2)
			continue
		}
		res[i] = make([]content, 1)
	}

	res[0] = make([]content, 1)
	res[0][0] = content{
		Tag:  "text",
		Text: "服务名称: " + conf.project.Name,
	}

	commitVersion := conf.apply.BranchName
	if len(conf.apply.CommitVersion) > 0 {
		commitVersion = conf.apply.CommitVersion
	}
	res[1][0] = content{
		Tag:  "text",
		Text: "服务版本: " + commitVersion,
	}
	res[2][0] = content{
		Tag:  "text",
		Text: "提交信息：" + conf.commitMsg,
	}
	res[3][0] = content{
		Tag:  "text",
		Text: "实例数量: " + strconv.Itoa(conf.serverNums),
	}
	res[4][0] = content{
		Tag:  "text",
		Text: "运行环境: " + conf.groupName,
	}
	res[4][1] = content{
		Tag:  "a",
		Text: "部署链接",
		Href: applyLink(conf.apply.ID),
	}
	res[5][0] = content{
		Tag:  "text",
		Text: "部署详情: ",
	}
	res[5][1] = content{
		Tag:  "a",
		Text: "部署链接",
		Href: applyLink(conf.apply.ID),
	}
	res[6][0] = content{
		Tag:  "text",
		Text: "发布人: " + conf.userName,
	}

	if len(conf.apply.CommitVersion) > 0 {
		res[1] = append(res[1], content{
			Tag:  "text",
			Text: "commitVersion:" + conf.apply.CommitVersion,
		})
	}

	req.Content.Post.ZhCn.Content = res
	return &req, nil
}
