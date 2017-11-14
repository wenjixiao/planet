package main

import (
	"./wq"
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/golang/protobuf/proto"
	"io"
	"log"
	"net"
	"time"
)

const (
	HeaderSize = 4

	LevelK = 'k'
	LevelD = 'd'
	LevelP = 'p'

	Black = 0
	White = 1

	IdPoolSize = 100

	// game status
	Inited  = 0
	Running = 1
	Paused  = 2
	Ended   = 3

	InnerConnBroken = 0
	InnerConnClosed = 1
	InnerReconnectOk = 2
)

type Level struct {
	Num    int32
	Suffix rune //p,d,k
}

func (level *Level) String() string {
	return fmt.Sprint(level.Num) + string(level.Suffix)
}

/* 把级别量化 */
func (level *Level) GetMount() int32 {
	var LevelMinK int32 = 18
	var LevelMaxD int32 = 9
	var mount int32
	if level.Suffix == LevelK {
		//k级是从18k->1k逆序的
		mount = LevelMinK - level.Num + 1
	} else if level.Suffix == LevelD {
		mount = LevelMinK + level.Num
	} else {
		//it's LevelP
		mount = LevelMinK + LevelMaxD + level.Num
	}
	return mount
}

// we can give Rule from two player's condition,base on WaitCondition,we can make
// auto invite quickly and accurate
type WaitCondition struct {
	LevelDiff                            int32 //级别范围 0,同级；1，上下差1；3，上下差3
	MinSeconds, MaxSeconds               int32 //保留时间范围,if min<0&&max<0,不限制
	MinCountdown, MaxCountdown           int32 //读秒范围
	MinTimesRetent, MaxTimesRetent       int32 //保留次数范围
	MinSecondsPerTime, MaxSecondsPerTime int32 //每次保留时间范围
}

func ToWaitCondition(wc *wq.WaitCondition) *WaitCondition {
	return &WaitCondition{
		LevelDiff:         wc.GetLevelDiff(),
		MinSeconds:        wc.GetMinSeconds(),
		MaxSeconds:        wc.GetMaxSeconds(),
		MinCountdown:      wc.GetMinCountdown(),
		MaxCountdown:      wc.GetMaxCountdown(),
		MinTimesRetent:    wc.GetMinTimesRetent(),
		MaxTimesRetent:    wc.GetMaxTimesRetent(),
		MinSecondsPerTime: wc.GetMinSecondsPerTime(),
		MaxSecondsPerTime: wc.GetMaxSecondsPerTime(),
	}
}

func (wc *WaitCondition) ToMsg() *wq.WaitCondition {
	return &wq.WaitCondition{
		LevelDiff:         wc.LevelDiff,
		MinSeconds:        wc.MinSeconds,
		MaxSeconds:        wc.MaxSeconds,
		MinCountdown:      wc.MinCountdown,
		MaxCountdown:      wc.MaxCountdown,
		MinTimesRetent:    wc.MinTimesRetent,
		MaxTimesRetent:    wc.MaxTimesRetent,
		MinSecondsPerTime: wc.MinSecondsPerTime,
		MaxSecondsPerTime: wc.MaxSecondsPerTime,
	}
}

type Player struct {
	Pid            string
	Level          Level
	IsPlaying      bool           //是否正在对局
	IsAcceptInvite bool           //是否接受邀请
	WaitCond       *WaitCondition //等待对局条件
}

func (p *Player) ToMsg() *wq.Player {
	return &wq.Player{
		Pid:            p.Pid,
		Level:          p.Level.String(),
		IsPlaying:      p.IsPlaying,
		IsAcceptInvite: p.IsAcceptInvite,
		WaitCond:       p.WaitCond.ToMsg(),
	}
}

type PlayerSetting struct {
	IsAcceptInvite bool
	WaitCond       *WaitCondition
}

func (ps *PlayerSetting) ToMsg() *wq.PlayerSetting {
	return &wq.PlayerSetting{
		IsAcceptInvite: ps.IsAcceptInvite,
		WaitCond:       ps.WaitCond.ToMsg(),
	}
}

type ClientProxy struct {
	Conn          net.Conn     //read from conn
	Down          chan *wq.Msg //clients can write to down
	Player        *Player
	PlayingGames  []*Game // 正在下的棋
	WatchingGames []*Game // 观看的棋
}

func (cp *ClientProxy) GetPlayingGamesMsg () (msgGames []*wq.Game) {
	for _,game := range cp.PlayingGames {
		msgGames = append(msgGames,game.ToMsg())
	}
	return
}

type ClientProxyMsg struct {
	Cp  *ClientProxy
	Msg *wq.Msg
}

type Stone struct {
	Seq   int32
	Color int32
	X, Y  int32
}

func (s *Stone) ToMsg() *wq.Stone {
	return &wq.Stone{
		Seq: s.Seq,
		Color: s.Color,
		X: s.X,
		Y: s.Y,
	}
}

type Counting struct {
	Countdown      int32 //读秒
	TimesRetent    int32 //保留次数
	SecondsPerTime int32 //每次保留时间
}

/* Msg to Counting */
func ToCounting(wc *wq.Counting) *Counting {
	return &Counting{
		Countdown:      wc.GetCountdown(),
		TimesRetent:    wc.GetTimesRetent(),
		SecondsPerTime: wc.GetSecondsPerTime(),
	}
}

func (c *Counting) ToMsg() *wq.Counting {
	return &wq.Counting{
		Countdown:      c.Countdown,
		TimesRetent:    c.TimesRetent,
		SecondsPerTime: c.SecondsPerTime,
	}
}

type InviteCondition struct {
	LevelDiff int32    //级别范围 0,同级；1，上下差1；3，上下差3
	Seconds   int32    //保留时间
	Counting  Counting //读秒
}

/* Msg to InviteCondition */
func ToInviteCondition(ic *wq.InviteCondition) *InviteCondition {
	return &InviteCondition{
		LevelDiff: ic.GetLevelDiff(),
		Seconds:   ic.GetSeconds(),
		Counting:  *ToCounting(ic.GetCounting()),
	}
}

func (ic *InviteCondition) ToMsg() *wq.InviteCondition {
	return &wq.InviteCondition{
		LevelDiff: ic.LevelDiff,
		Seconds:   ic.Seconds,
		Counting:  ic.Counting.ToMsg(),
	}
}

// when we invite or waiting ,we should give the playing game condition with proto
// so,we can fast dive into game,not useless time proto dialogs
type Rule struct {
	Handicap int32    //让子
	Komi     float32  //贴目
	Seconds  int32    //时间
	Counting Counting //读秒
}

func (r *Rule) ToMsg() *wq.Rule {
	return &wq.Rule{
		Handicap: r.Handicap,
		Komi: r.Komi,
		Seconds: r.Seconds,
		Counting: r.Counting.ToMsg(),
	}
}

func (r *Rule) MakeTime() Time {
	return Time{
		Seconds:  r.Seconds,
		Counting: r.Counting,
	}
}

type Result struct {
	HasWinner          bool
	WinnerColor        int32
	MiddleWin, TimeWin bool
	Howmuch            float32
}

func (r *Result) ToMsg() *wq.Result {
	return &wq.Result{
		HasWinner: r.HasWinner,
		WinnerColor: r.WinnerColor,
		MiddleWin: r.MiddleWin,
		TimeWin: r.TimeWin,
		Howmuch: r.Howmuch,
	}
}

type Time struct {
	Seconds  int32
	Counting Counting
}

func (t *Time) ToMsg() *wq.Time {
	return &wq.Time{
		Seconds: t.Seconds,
		Counting: t.Counting.ToMsg(),
	}
}

type Game struct {
	Id           int32
	Rule         *Rule
	Status       int32
	LastColor    int32 // last stone color,knowing who's time is flowing
	Stones       []Stone
	Result       Result
	PlayerCps    []*ClientProxy
	Times        []Time
	WatcherCps   []*ClientProxy
	MsgPipe      chan *ClientProxyMsg
	InnerMsgPipe chan *InnerMsg
}

func (g *Game) GetTimesMsg() (times []*wq.Time) {
	for _,t := range g.Times {
		times = append(times,t.ToMsg())
	}
	return
}

func (g *Game) GetPlayersMsg() (players []*wq.Player) {
	for _,cp := range g.PlayerCps {
		players = append(players,cp.Player.ToMsg())
	}
	return
}

func (g *Game) GetWatchersMsg() (watchers []*wq.Player) {
	for _,cp := range g.WatcherCps {
		watchers = append(watchers,cp.Player.ToMsg())
	}
	return
}

func (g *Game) GetStonesMsg() (stones []*wq.Stone) {
	for _,stone := range g.Stones {
		stones = append(stones,stone.ToMsg())
	}
	return
}

func (g *Game) ToMsg() *wq.Game {
	return &wq.Game{
		Id: g.Id,
		Rule: g.Rule.ToMsg(),
		Status: g.Status,
		LastColor: g.LastColor,
		Stones: g.GetStonesMsg(),
		Result: g.Result.ToMsg(),
		Players: g.GetPlayersMsg(),
		Times: g.GetTimesMsg(),
		Watchers: g.GetWatchersMsg(),
	}
}

type InnerMsg struct {
	MsgType     int32
	clientProxy *ClientProxy
}

var (
	serverPipe         chan *ClientProxyMsg = make(chan *ClientProxyMsg)
	serverInnerPipe    chan *InnerMsg       = make(chan *InnerMsg)
	games              []*Game              = []*Game{}
	clientProxys       []*ClientProxy       = []*ClientProxy{}
	brokenClientProxys []*ClientProxy       = []*ClientProxy{}
	idpool             *IdPool              = NewIdPool(IdPoolSize)
)

func NewGame(cond *InviteCondition, cp1 *ClientProxy, cp2 *ClientProxy) *Game {
	game := &Game{Id: idpool.GetId()}
	game.Status = Inited
	game.Rule = NewRule(cond, cp1.Player, cp2.Player)
	if game.Rule.Handicap == 0 {
		game.LastColor = Black
	} else {
		// handicap > 0
		game.LastColor = White
	}
	game.PlayerCps = []*ClientProxy{cp1, cp2}
	game.Times = []Time{game.Rule.MakeTime(), game.Rule.MakeTime()}
	log.Printf("game=%v\n", game)
	return game
}

/* init status loop */
func GameInitedLoop(game *Game) {
	log.Println("***game Inited status***")
	timer := time.NewTimer(time.Second)
	/* 倒数五个数 */
	var num int32 = 5
	for num > 0 {
		select {
		case <-timer.C:
			for _, clientProxy := range game.PlayerCps {
				clientProxy.Down <- &wq.Msg{
					Union: &wq.Msg_CountBackward{
						&wq.CountBackward{Id: game.Id, Num: num}},
				}
			}
			num--
		case im := <-game.InnerMsgPipe:
			// maybe conn broken or socket closed(user leave)
			switch im.MsgType {
			case InnerConnBroken:

			case InnerConnClosed:
			}
		}
	}
	// 没什么异常的话，转入running状态
	GameRunningLoop(game)
}

/* paused status loop */
func GamePausedLoop(game *Game) {
	log.Println("***game Paused status***")
	game.Status = Paused
}

/* running status loop */
func GameRunningLoop(game *Game) {
	log.Println("***game Running status***")
	game.Status = Running
	// game have a inner timer,it always run like in real world
	timer := time.NewTimer(time.Second)
	for {
		select {
		case msg := <-game.MsgPipe:
			log.Printf("get msg for game:%v\n", msg)
		case <-game.InnerMsgPipe:
		case <-timer.C:
			// 读秒
		}
	}
}

type IdPool struct {
	Size int32
	Nums []int32
}

func NewIdPool(size int32) *IdPool {
	idpool := &IdPool{Size: size}
	var i int32
	for i = 1; i <= size; i++ {
		idpool.Nums = append(idpool.Nums, i)
	}
	return idpool
}

func (idpool *IdPool) GetId() int32 {
	if len(idpool.Nums) > 0 {
		idpool.Nums = idpool.Nums[1:]
		return idpool.Nums[0]
	} else {
		idpool.Size += 1
		idpool.Nums = append(idpool.Nums, idpool.Size)
		return idpool.Nums[0]
	}
}

func (idpool *IdPool) PutId(id int32) {
	idpool.Nums = append(idpool.Nums, id)
}

func GetPlayer(player *Player, pid string, passwd string) bool {
	if pid == "wenjixiao" {
		player.Pid = "wenjixiao"
		player.Level = Level{3, LevelD}
		player.IsPlaying = false
		player.IsAcceptInvite = true
		player.WaitCond = DefaultWaitCondition()
		return true
	} else {
		return false
	}
}

func DefaultWaitCondition() *WaitCondition {
	return &WaitCondition{
		LevelDiff:  0,
		MinSeconds: 1200, MaxSeconds: 1200,
		MinCountdown: 30, MaxCountdown: 30,
		MinTimesRetent: 3, MaxTimesRetent: 3,
		MinSecondsPerTime: 60, MaxSecondsPerTime: 60,
	}
}

/*
min-----max
	 min------max
*/
func HasIntersection(min1, max1, min2, max2 int32) bool {
	var b bool = true
	if min1 > max2 || max1 < min2 {
		b = false
	}
	return b
}

func LevelRange(level Level, diff int32) (mountMin int32, mountMax int32) {
	mount := level.GetMount()
	mountMin = mount - diff
	mountMax = mount + diff
	//range should in [1,36],36 = 18k+9d+9p
	if mountMin < 1 {
		mountMin = 1
	}
	if mountMax > 36 {
		mountMax = 36
	}
	return
}

func ValueInRange(v, min, max int32) bool {
	return v >= min && v <= max
}

/* inviting from p1,p2 is waiting */
func ConditionMatch(cond *InviteCondition, p1 *Player, p2 *Player) bool {
	var wd *WaitCondition = p2.WaitCond
	//level cond
	min1, max1 := LevelRange(p1.Level, cond.LevelDiff)
	min2, max2 := LevelRange(p2.Level, wd.LevelDiff)
	levelCond := HasIntersection(min1, max1, min2, max2)
	//seconds cond
	secondsCond := cond.Seconds >= wd.MinSeconds && cond.Seconds <= wd.MaxSeconds
	//counting cond
	countdown := cond.Counting.Countdown
	timesRetent := cond.Counting.TimesRetent
	secondsPerTime := cond.Counting.SecondsPerTime

	countdownCond := ValueInRange(countdown, wd.MinCountdown, wd.MaxCountdown)
	timesRetentCond := ValueInRange(timesRetent, wd.MinTimesRetent, wd.MaxTimesRetent)
	secondsPerTimeCond := ValueInRange(secondsPerTime, wd.MinSecondsPerTime, wd.MaxSecondsPerTime)

	return levelCond && secondsCond && countdownCond && timesRetentCond && secondsPerTimeCond
}

/* 绝对值 */
func Abs(n int32) int32 {
	if n >= 0 {
		return n
	} else {
		return n * (-1)
	}
}

/*
inviting from p1 to p2
贴目和让子值自动生成
*/
func NewRule(cond *InviteCondition, p1 *Player, p2 *Player) *Rule {
	rule := &Rule{}
	rule.Seconds = cond.Seconds
	rule.Counting = cond.Counting

	mount1 := p1.Level.GetMount()
	mount2 := p2.Level.GetMount()
	if mount1 == mount2 {
		rule.Handicap = 0
		rule.Komi = 6.5
	} else {
		rule.Handicap = Abs(mount1 - mount2)
		rule.Komi = float32(rule.Handicap)
	}
	return rule
}

func ServerLoop() {
	for {
		select {
		case clientProxyMsg := <-serverPipe:
			log.Printf("msg from serverPipe: %v\n", clientProxyMsg)
			msg := clientProxyMsg.Msg
			clientProxy := clientProxyMsg.Cp
			switch msg.Union.(type) {
			// login
			case *wq.Msg_Login:
				login := msg.GetLogin()
				player := &Player{}
				if GetPlayer(player, login.Pid, login.Passwd) {
					clientProxy.Player = player
					/* the player broken before,reconnect now */
					if index := SearchClientProxyById(brokenClientProxys,clientProxy.Player.Pid);index >=0 {
						// get the broken client proxy
						brokenClientProxy := brokenClientProxys[index]
						// delete old broken client proxy from brokenClientProxys
						brokenClientProxys = append(brokenClientProxys[:index],brokenClientProxys[index+1:]...)
						// move the game's data to new client proxy
						clientProxy.PlayingGames = brokenClientProxy.PlayingGames
						// remove the broken client proxy from games and replace by new client proxy
						for _,game := range clientProxy.PlayingGames {
							index := SearchClientProxyByAddr(game.PlayerCps,brokenClientProxy)
							if index >= 0 { game.PlayerCps[index] = clientProxy }
						}
					}
					// return msg to client,means login ok,if reconnect,have games data
					clientProxy.Down <- &wq.Msg{
						Union: &wq.Msg_LoginReturnOk{
							&wq.LoginReturnOk{
								Player: player.ToMsg(),
								PlayingGames: clientProxy.GetPlayingGamesMsg(),
							},
						},
					}
					// if have playing games ,tell every game,the reconnect event
					for _,game := range clientProxy.PlayingGames {
						game.InnerMsgPipe <- &InnerMsg{InnerReconnectOk,clientProxy} 
					}
				} else {
					msgFail := &wq.Msg{
						Union: &wq.Msg_LoginReturnFail{
							&wq.LoginReturnFail{
								Reason: "pid or password error!",
							},
						},
					}
					clientProxy.Down <- msgFail
				}
				// PlayerSetting
			case *wq.Msg_PlayerSetting:
				playerSetting := msg.GetPlayerSetting()
				clientProxy.Player.IsAcceptInvite = playerSetting.GetIsAcceptInvite()
				clientProxy.Player.WaitCond = ToWaitCondition(playerSetting.GetWaitCond())
				// InviteAuto
			case *wq.Msg_InviteAuto:
				inviteCondition := ToInviteCondition(msg.GetInviteAuto().GetInviteCondition())
				fmt.Printf("inviteCondition=%v\n", inviteCondition)
				InviteAutoMatch(inviteCondition, clientProxy)
				// InvitePlayer
			case *wq.Msg_InvitePlayer:
				targetPid := msg.GetInvitePlayer().GetPid()
				inviteCondition := ToInviteCondition(msg.GetInvitePlayer().GetInviteCondition())
				InvitePlayerMatch(inviteCondition, clientProxy, targetPid)
			default:
			} //switch
		case innerMsg := <-serverInnerPipe:
			log.Printf("inner msg: %v\n", innerMsg)
		}
	} //for
}

func SearchClientProxy(clientProxys []*ClientProxy, predicate func(clientProxy *ClientProxy) bool) int {
	for index, cp := range clientProxys {
		if predicate(cp) {
			// finded
			return index
		}
	}
	// not find
	return -1
}

/* search ClientProxy by pid,return index */
func SearchClientProxyById(clientProxys []*ClientProxy, pid string) int {
	return SearchClientProxy(clientProxys, func(clientProxy *ClientProxy) bool {
		return clientProxy.Player.Pid == pid
	})
}

/* search ClientProxy by pid,return index */
func SearchClientProxyByAddr(clientProxys []*ClientProxy, cp *ClientProxy) int {
	return SearchClientProxy(clientProxys, func(clientProxy *ClientProxy) bool {
		return clientProxy == cp
	})
}

func IsClientProxyBroken(clientProxy *ClientProxy) bool {
	return SearchClientProxy(brokenClientProxys, func(cp *ClientProxy) bool { return clientProxy == cp }) >= 0 
}

func NewInviteFail(reason string) *wq.Msg {
	return &wq.Msg{Union: &wq.Msg_InviteFail{&wq.InviteFail{Reason: reason}}}
}

func InviteAutoMatch(cond *InviteCondition, cp *ClientProxy) {
	for _, clientProxy := range clientProxys {
		if ConditionMatch(cond, cp.Player, clientProxy.Player) &&
			!clientProxy.Player.IsPlaying &&
			cp != clientProxy {
			_ = NewGame(cond, cp, clientProxy)
			// go (make(chan *wq.Msg), game)
			return
		}
	}
	cp.Down <- NewInviteFail("no condition matched player")
}

func InvitePlayerMatch(cond *InviteCondition, cp *ClientProxy, pid string) {
	if index := SearchClientProxyById(clientProxys, pid); index >= 0 {
		clientProxy := clientProxys[index]
		if ConditionMatch(cond, cp.Player, clientProxy.Player) {
			if !clientProxy.Player.IsPlaying {
				NewGame(cond, cp, clientProxy)
			} else {
				cp.Down <- NewInviteFail("the player is playing")
			}
		} else {
			cp.Down <- NewInviteFail("condition not match")
		}
	} else {
		cp.Down <- NewInviteFail("can't find the player")
	}
}

func ListenLoop() {
	l, err := net.Listen("tcp", ":5678")
	defer l.Close()
	if err != nil {
		log.Fatal(err)
	}

	go ServerLoop()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		clientProxy := &ClientProxy{Conn: conn, Down: make(chan *wq.Msg)}
		clientProxys = append(clientProxys, clientProxy)
		go clientProxy.HandleUp()
		go clientProxy.HandleDown()
	}
	log.Println("listen loop exit,all exit!")
}

func (clientProxy *ClientProxy) HandleUp() {
	defer clientProxy.Conn.Close()

	const MSG_BUF_LEN = 1024 * 1024 //1MB
	const READ_BUF_LEN = 1024       //1KB

	log.Printf("Client: %s\n", clientProxy.Conn.RemoteAddr())

	msgBuf := bytes.NewBuffer(make([]byte, 0, MSG_BUF_LEN))
	readBuf := make([]byte, READ_BUF_LEN)

	head := uint32(0)
	bodyLen := 0 //bodyLen is a flag,when readed head,but body'len is not enougth

	for IsClientProxyBroken(clientProxy) {
		n, err := clientProxy.Conn.Read(readBuf)
		if err != nil {
			if err == io.EOF {
				clientProxy.ConnClosed()
			} else {
				log.Printf("Read error: %s\n", err)
				clientProxy.ConnBroken()
			}
			break
		}
		_, err = msgBuf.Write(readBuf[:n])

		if err != nil {
			log.Fatalf("Buffer write error: %s\n", err)
		}

		for {
			//read the msg head
			if bodyLen == 0 && msgBuf.Len() >= HeaderSize {
				err := binary.Read(msgBuf, binary.LittleEndian, &head)
				if err != nil {
					log.Printf("msg head Decode error: %s\n", err)
				}
				bodyLen = int(head)

				if bodyLen > MSG_BUF_LEN {
					log.Fatalf("msg body too long: %d\n", bodyLen)
				}
			}
			//has head,now read body
			if bodyLen > 0 && msgBuf.Len() >= bodyLen {
				clientProxy.ProcessMsg(msgBuf.Next(bodyLen))
				bodyLen = 0
			} else {
				//msgBuf.Len() < bodyLen ,one msg receiving is not complete
				//need to receive again
				break
			}
		} //for of msg buf
	} //for of conn read
}

func (clientProxy *ClientProxy) HandleDown() {
	for IsClientProxyBroken(clientProxy) {
		msg := <-clientProxy.Down
		clientProxy.WriteMsg(msg)
	}
}

func (clientProxy *ClientProxy) ConnClosed() {
	log.Println("****line closed****")
	serverInnerPipe <- &InnerMsg{InnerConnClosed, clientProxy}
}

/* 每一次有新连接到来时，都新建一个clientproxy,所以，连接断裂时，要抛弃旧的 */
func (clientProxy *ClientProxy) ConnBroken() {
	//when conn broken,we should reset the message buffer too!
	if index := SearchClientProxyByAddr(clientProxys, clientProxy); index > 0 {
		brokenClientProxys = append(brokenClientProxys, clientProxys[index])
		clientProxys = append(clientProxys[:index], clientProxys[index+1:]...)
	} else {
		log.Fatal("can't find *clientProxy in server clientProxys")
	}
	innerMsg := &InnerMsg{InnerConnBroken, clientProxy}
	// tell server
	serverInnerPipe <- innerMsg
	// tell game
	for _, game := range append(clientProxy.PlayingGames, clientProxy.WatchingGames...) {
		game.InnerMsgPipe <- innerMsg
	}
	log.Println("****line broken****")
}

func (clientProxy *ClientProxy) ProcessMsg(msgBytes []byte) {
	log.Println("------------------------")
	msg := &wq.Msg{}
	err := proto.Unmarshal(msgBytes, msg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("#the MSG#: %s\n", msg)
	// 分发到相应的地方
	clientProxy.DispatchMsg(msg)
}

/*
 *todo* here we should dispatch the msg to 1:Server or a 2:Game
 1,确定是server的，还是game的；2,确定是那个game的
*/
func (clientProxy *ClientProxy) DispatchMsg(msg *wq.Msg) {
	cpm := &ClientProxyMsg{clientProxy, msg}
	gameId := msg.GetId()
	if gameId > 0 {
		// give msg to #gameId# game
		for _, game := range games {
			if game.Id == gameId {
				game.MsgPipe <- cpm
				break
			}
		}
	} else {
		// give msg to server
		serverPipe <- cpm
	}
}

func AddHeader(msgBytes []byte) []byte {
	head := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(head, uint32(len(msgBytes)))
	return append(head, msgBytes...)
}

func (clientProxy *ClientProxy) WriteMsg(msg *wq.Msg) {
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		log.Fatalf("proto marshal error: %s\n", err)
	}
	_, err = clientProxy.Conn.Write(AddHeader(msgBytes))
	if err != nil {
		log.Fatalf("conn write error: %s\n", err)
	}
}

func main() {
	ListenLoop()
}
