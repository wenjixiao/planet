package main

import (
	"bytes"
	"encoding/binary"
	"github.com/golang/protobuf/proto"
	"io"
	"log"
	"net"
	"./wq"
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

type Player struct {
	Pid            string
	Level          Level
	IsPlaying      bool      //是否正在对局
	IsAcceptInvite bool      //是否接受邀请
	Cond           Condition //对局条件
}

// i should know the minLevel and maxLevel in my condition
func (p *Player) LevelRange() (mountMin int, mountMax int) {
	mount := p.Level.GetMount()
	diff := p.Cond.LevelDiff
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

// we can give Rule from two player's condition,base on Condition,we can make
// auto invite quickly and accurate
type Condition struct {
	LevelDiff                            int //级别范围 0,同级；1，上下差1；3，上下差3
	HandicapDiff                         int //0,不让=<按规则来>；规则之上{1，让你一个，2，让你2个；-1，你让我一个}
	KomiDiff                             int //0,as the rule;1,add 1 over rule;-1,sub 1 over rule
	MinSeconds, MaxSeconds               int //保留时间范围,if min<0&&max<0,不限制
	MinCountdown, MaxCountdown           int //读秒范围
	MinTimesRetent, MaxTimesRetent       int //保留次数范围
	MinSecondsPerTime, MaxSecondsPerTime int //每次保留时间范围
}

// when we invite or waiting ,we should give the playing game condition with proto
// so,we can fast dive into game,not useless time proto dialogs
type Rule struct {
	Handicap int      //让子
	Komi     float32  //贴目
	Counting Counting //读秒
	Seconds  int      //时间
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
		player.Cond = DefaultCondition()
		return true
	} else {
		return false
	}
}

func DefaultCondition() Condition {
	return Condition{
		LevelDiff: 0, HandicapDiff: 0, KomiDiff: 0,
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

func DoesMatch(p1 Player, p2 Player) bool {
	var b bool = true
	min1, max1 := p1.LevelRange()
	min2, max2 := p2.LevelRange()
	levelMatch := HasIntersection(min1, max1, min2, max2)
	return b && levelMatch
}

/* func makeRule(p1 Player,p2 Player) Rule {
	rule := Rule{}
	heheeh
}
*/
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
						&wq.LoginReturnOk{
							Player: &wq.Player{Pid: player.Pid, Level: player.Level.String()},
						},
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
		log.Printf("conn's type is: %T\n",conn)
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
		}
	}
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