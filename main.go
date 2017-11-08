package main

import (
	"./wq"
	"bytes"
	"encoding/binary"
	"github.com/golang/protobuf/proto"
	"io"
	"log"
	"net"
	"strconv"
)

const (
	HeaderSize = 4

	LevelK = 'k'
	LevelD = 'd'
	LevelP = 'p'

	Black = 0
	White = 1
)

type Level struct {
	Num    int
	Suffix rune //p,d,k
}

func (level *Level) String() string {
	return strconv.Itoa(level.Num) + string(level.Suffix)
}

/* 把级别量化 */
func (level *Level) GetMount() int {
	LevelMinK := 18
	LevelMaxD := 9
	var mount int
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
	LevelDiff                            int //级别范围 0,同级；1，上下差1；3，上下差3
	MinSeconds, MaxSeconds               int //保留时间范围,if min<0&&max<0,不限制
	MinCountdown, MaxCountdown           int //读秒范围
	MinTimesRetent, MaxTimesRetent       int //保留次数范围
	MinSecondsPerTime, MaxSecondsPerTime int //每次保留时间范围
}

func (wc *WaitCondition) ToMsg() *wq.WaitCondition {
	return &wq.WaitCondition{
		LevelDiff:         int32(wc.LevelDiff),
		MinSeconds:        int32(wc.MinSeconds),
		MaxSeconds:        int32(wc.MaxSeconds),
		MinCountdown:      int32(wc.MinCountdown),
		MaxCountdown:      int32(wc.MaxCountdown),
		MinTimesRetent:    int32(wc.MinTimesRetent),
		MaxTimesRetent:    int32(wc.MaxTimesRetent),
		MinSecondsPerTime: int32(wc.MinSecondsPerTime),
		MaxSecondsPerTime: int32(wc.MaxSecondsPerTime),
	}
}

type Player struct {
	Pid            string
	Level          Level
	IsPlaying      bool          //是否正在对局
	IsAcceptInvite bool          //是否接受邀请
	WaitCond       WaitCondition //等待对局条件
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
	WaitCond       WaitCondition
}

func (ps *PlayerSetting) ToMsg() *wq.PlayerSetting {
	return &wq.PlayerSetting{
		IsAcceptInvite: ps.IsAcceptInvite,
		WaitCond:       ps.WaitCond.ToMsg(),
	}
}

type ClientProxy struct {
	Conn   net.Conn    //read from conn
	Down   chan wq.Msg //clients can write to down
	Player *Player
}

type ClientProxyMsg struct {
	Cp  *ClientProxy
	Msg *wq.Msg
}

type Stone struct {
	Seq   int
	Color int
	X, Y  int
}

type Counting struct {
	Countdown      int //读秒
	TimesRetent    int //保留次数
	SecondsPerTime int //每次保留时间
}

func (c *Counting) ToMsg() *wq.Counting {
	return &wq.Counting{
		Countdown:      int32(c.Countdown),
		TimesRetent:    int32(c.TimesRetent),
		SecondsPerTime: int32(c.SecondsPerTime),
	}
}

type InviteCondition struct {
	LevelDiff int      //级别范围 0,同级；1，上下差1；3，上下差3
	Seconds   int      //保留时间
	Counting  Counting //读秒
}

func (ic *InviteCondition) ToMsg() *wq.InviteCondition {
	return &wq.InviteCondition{
		LevelDiff: int32(ic.LevelDiff),
		Seconds:   int32(ic.Seconds),
		Counting:  ic.Counting.ToMsg(),
	}
}

// when we invite or waiting ,we should give the playing game condition with proto
// so,we can fast dive into game,not useless time proto dialogs
type Rule struct {
	Handicap int      //让子
	Komi     float32  //贴目
	Seconds  int      //时间
	Counting Counting //读秒
}

type Result struct {
	HasWinner          bool
	WinnerColor        int
	MiddleWin, TimeWin bool
	Howmuch            float32
}

type Time struct {
	Count   Counting
	Seconds int
}

type Game struct {
	Id         int32
	LastColor  int
	Rule       Rule
	Stones     []Stone
	Result     Result
	PlayerCps  []*ClientProxy
	Times      []Time
	WatcherCps []*ClientProxy
}

var (
	serverPipe   chan ClientProxyMsg = make(chan ClientProxyMsg)
	gamePipes    map[int]chan wq.Msg = map[int]chan wq.Msg{}
	clientProxys []*ClientProxy      = []*ClientProxy{}
)

func CreateGame(cp1 *ClientProxy, cp2 *ClientProxy) {
	game := &Game{}
	log.Printf("game=%v\n", game)
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

func DefaultWaitCondition() WaitCondition {
	return WaitCondition{
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
func HasIntersection(min1, max1, min2, max2 int) bool {
	var b bool = true
	if min1 > max2 || max1 < min2 {
		b = false
	}
	return b
}

func LevelRange(level Level, diff int) (mountMin int, mountMax int) {
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

func ValueInRange(v, min, max int) bool {
	return v >= min && v <= max
}

/* inviting from p1,p2 is waiting */
func ConditionMatch(cond InviteCondition, p1 Player, p2 Player) bool {
	var wd WaitCondition = p2.WaitCond
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
func abs(n int) int {
	if n >= 0 {
		return n
	} else {
		return n * (-1)
	}
}

/*
inviting from p1 to p2
贴目和让子自动生成
*/
func makeRule(cond InviteCondition, p1 Player, p2 Player) Rule {
	rule := Rule{}
	rule.Seconds = cond.Seconds
	rule.Counting = cond.Counting

	mount1 := p1.Level.GetMount()
	mount2 := p2.Level.GetMount()
	if mount1 == mount2 {
		rule.Handicap = 0
		rule.Komi = 6.5
	} else {
		rule.Handicap = abs(mount1 - mount2)
		rule.Komi = float32(rule.Handicap)
	}
	return rule
}

func ServerLoop() {
	for {
		log.Println("read serverPipe")
		clientProxyMsg := <-serverPipe
		log.Printf("msg from serverPipe: %v\n", clientProxyMsg)
		msg := clientProxyMsg.Msg
		clientProxy := clientProxyMsg.Cp
		switch msg.Union.(type) {
		case *wq.Msg_Login:
			login := msg.GetLogin()
			player := &Player{}
			if GetPlayer(player, login.Pid, login.Passwd) {
				clientProxy.Player = player
				msgOk := &wq.Msg{
					Union: &wq.Msg_LoginReturnOk{
						&wq.LoginReturnOk{Player: player.ToMsg()},
					},
				}
				clientProxy.Down <- *msgOk
			} else {
				msgFail := &wq.Msg{
					Union: &wq.Msg_LoginReturnFail{
						&wq.LoginReturnFail{
							Reason: "pid or password error!",
						},
					},
				}
				clientProxy.Down <- *msgFail
			}
			//other cases
		} //switch
	} //for
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

		clientProxy := &ClientProxy{Conn: conn, Down: make(chan wq.Msg)}

		clientProxys = append(clientProxys, clientProxy)

		go HandleUp(clientProxy)
		go HandleDown(clientProxy)
	}
	log.Println("listen loop exit,all exit!")
}

func HandleUp(clientProxy *ClientProxy) {
	defer clientProxy.Conn.Close()

	const MSG_BUF_LEN = 1024 * 1024 //1MB
	const READ_BUF_LEN = 1024       //1KB

	log.Printf("Client: %s\n", clientProxy.Conn.RemoteAddr())

	msgBuf := bytes.NewBuffer(make([]byte, 0, MSG_BUF_LEN))
	readBuf := make([]byte, READ_BUF_LEN)

	head := uint32(0)
	bodyLen := 0 //bodyLen is a flag,when readed head,but body'len is not enougth

	for {
		n, err := clientProxy.Conn.Read(readBuf)
		if err != nil {
			if err == io.EOF {
				ConnBroken(clientProxy.Conn)
			} else {
				log.Fatalf("Read error: %s\n", err)
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
				ProcessMsg(msgBuf.Next(bodyLen), clientProxy)
				bodyLen = 0
			} else {
				//msgBuf.Len() < bodyLen ,one msg receiving is not complete
				//need to receive again
				break
			}
		} //for of msg buf
	} //for of conn read
}

func HandleDown(clientProxy *ClientProxy) {
	for {
		msg := <-clientProxy.Down
		WriteMsg(&msg, clientProxy.Conn)
	}
}

func ConnBroken(conn net.Conn) {
	//when conn broken,we should reset the message buffer too!
	log.Println("****line broken****")
}

func ProcessMsg(msgBytes []byte, clientProxy *ClientProxy) {
	log.Println("------------------------")
	msg := &wq.Msg{}
	err := proto.Unmarshal(msgBytes, msg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("#the MSG#: %s\n", msg)
	serverPipe <- ClientProxyMsg{clientProxy, msg}
	//todo here we should dispatch the msg to 1:Server or a 2:Game
	log.Println("write msg to serverPipe,ok")
	// WriteMsg(msg, clientProxy.conn)
}

func AddHeader(msgBytes []byte) []byte {
	head := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(head, uint32(len(msgBytes)))
	return append(head, msgBytes...)
}

func WriteMsg(msg *wq.Msg, conn net.Conn) {
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		log.Fatalf("proto marshal error: %s\n", err)
	}
	_, err = conn.Write(AddHeader(msgBytes))
	if err != nil {
		log.Fatalf("conn write error: %s\n", err)
	}
}

func main() {
	ListenLoop()
}
