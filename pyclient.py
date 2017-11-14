import socket
import struct
import wx
from wx.lib.pubsub import pub
import threading
from socketmsg import SocketReader
import wq_pb2 as wq

player = None
# exitFlag accessed by main thread and socket read thread,so need a lock
exitFlag = False
exitFlagLock = threading.Lock()

class ThreadSocketReader(threading.Thread):
    def __init__(self,socket):
		threading.Thread.__init__(self)
		self.socketBufSize = 1024
		self.socket = socket
		self.buf = bytes()
		self.headSize = 4
    def run(self):
		self.readMessage()    	
    def readMessage(self):
        print("start read socket...")
        try:
            while True:
            	exitFlagLock.acquire()
            	if exitFlag:
            		break
            	exitFlagLock.release()
            	# when the main thread 'shutdown' the socket,recv return immediately,NOT exception
                data = self.socket.recv(self.socketBufSize)
                if not data: break
                self.buf += data
                while True:
                    if len(self.buf) < self.headSize:
                        print("dataSize < headSize!")
                        break
                    bodySize, = struct.unpack('<I',self.buf[:self.headSize])
                    print("bodySize={}".format(bodySize))
                    if len(self.buf) < self.headSize+bodySize:
                        print("message data not enougth!")
                        break
                    bin = self.buf[self.headSize:self.headSize+bodySize]
                    self.processMessage(bin)
                    self.buf = self.buf[self.headSize+bodySize:]
        except Exception,e: 
        	print e
        finally:
			self.socket.close()
			print "socket reader closed"
    def send(self,bin):
        header = struct.pack('I',len(bin))
        self.socket.sendall(header+bin)
    def processMessage(self,bin):
        print("---------------")
        s = wq.Msg()
        s.ParseFromString(bin)
        pub.sendMessage('message_topic',message=s)

# Bind(event, handler, source=None, id=wx.ID_ANY, id2=wx.ID_ANY)
class WeiqiClient(wx.Frame):
    def __init__(self, parent, title):
        super(WeiqiClient, self).__init__(parent, title=title, size=(600, 400))
        self.InitUI()
        self.Centre()
        self.Show()
    def InitUI(self):
        panel = wx.Panel(self)
        panel.SetBackgroundColour('#2F4F2F')
        vbox = wx.BoxSizer(wx.VERTICAL)

        self.connectBt = wx.Button(panel,wx.ID_ANY,'Connect')
        self.loginMsgBt = wx.Button(panel,wx.ID_ANY,'Login')
        self.inviteAutoMsgBt = wx.Button(panel,wx.ID_ANY,'InviteAuto')
        self.invitePlayerMsgBt = wx.Button(panel,wx.ID_ANY,'InvitePlayer')
        self.playerSettingMsgBt = wx.Button(panel,wx.ID_ANY,'player setting')
        
        self.Bind(wx.EVT_CLOSE, self.OnCloseWindow)
        self.Bind(wx.EVT_BUTTON,self.OnConnectBt,source=self.connectBt)
        self.Bind(wx.EVT_BUTTON,self.OnLoginMsgBt,source=self.loginMsgBt)
        self.Bind(wx.EVT_BUTTON,self.OnInviteAutoMsgBt,source=self.inviteAutoMsgBt)
        self.Bind(wx.EVT_BUTTON,self.OnInvitePlayerMsgBt,source=self.invitePlayerMsgBt)
        self.Bind(wx.EVT_BUTTON,self.OnPlayerSettingMsgBt,source=self.playerSettingMsgBt)
        
        vbox.Add(self.connectBt, 1, wx.EXPAND)
        vbox.Add(self.loginMsgBt, 1, wx.EXPAND)
        vbox.Add(self.inviteAutoMsgBt, 1, wx.EXPAND)
        vbox.Add(self.invitePlayerMsgBt, 1, wx.EXPAND)
        vbox.Add(self.playerSettingMsgBt, 1, wx.EXPAND)
        
        panel.SetSizer(vbox)

        pub.subscribe(self.receivedMessage,"message_topic")
    # msg type we can send
    def OnConnectBt(self,e):
        self.connect()
    def OnLoginMsgBt(self,e):
        msg = wq.Msg()
        msg.login.pid = "wenjixiao"
        msg.login.passwd = "123456"
        self.send(msg)
    def OnInviteAutoMsgBt(self,e):
    	msg = wq.Msg()
    	msg.inviteAuto.inviteCondition.levelDiff = 3
    	msg.inviteAuto.inviteCondition.seconds = 1200
    	msg.inviteAuto.inviteCondition.counting.countdown = 30
    	msg.inviteAuto.inviteCondition.counting.timesRetent = 3
    	msg.inviteAuto.inviteCondition.counting.secondsPerTime = 60
    	self.send(msg)
    def OnInvitePlayerMsgBt(self,e):
    	msg = wq.Msg()
    	msg.invitePlayer.pid = "zhongzhong"
    	msg.invitePlayer.inviteCondition.levelDiff = 3
    	msg.invitePlayer.inviteCondition.seconds = 1200
    	msg.invitePlayer.inviteCondition.counting.countdown = 30
    	msg.invitePlayer.inviteCondition.counting.timesRetent = 3
    	msg.invitePlayer.inviteCondition.counting.secondsPerTime = 60
    	self.send(msg)
    def OnPlayerSettingMsgBt(self,e):
    	msg = wq.Msg()
    	msg.playerSetting.isAcceptInvite = False
    	msg.playerSetting.waitCond.levelDiff = 0
    	msg.playerSetting.waitCond.minSeconds = 1200
    	msg.playerSetting.waitCond.maxSeconds = 1200
    	msg.playerSetting.waitCond.minCountdown = 30
    	msg.playerSetting.waitCond.maxCountdown = 60
    	msg.playerSetting.waitCond.minTimesRetent = 3
    	msg.playerSetting.waitCond.maxTimesRetent = 10
    	msg.playerSetting.waitCond.minSecondsPerTime = 60
    	msg.playerSetting.waitCond.maxSecondsPerTime = 60
    	self.send(msg)
    # messages we get
    def receivedMessage(self,message):
        print "received msg:",message
        unionType = message.WhichOneof('union')
        if unionType == 'loginReturnOk':
        	player = message.loginReturnOk.player
        elif unionType == 'loginReturnFail':
        	print "login failed,reason is : ",message.loginReturnFail.reason
        elif unionType == 'inviteFail':
        	print "invite failed,reason is : ",message.inviteFail.reason
        else:
        	print "***no matched union type***: ",message
    def send(self,msg):
    	bin = msg.SerializeToString()
        header = struct.pack('I',len(bin))
        self.sock.sendall(header+bin)
    def connect(self):
        self.addr = ('127.0.0.1',5678)
        self.sock = socket.socket(socket.AF_INET,socket.SOCK_STREAM)
        self.sock.connect(self.addr)
        ThreadSocketReader(self.sock).start()
    def OnCloseWindow(self,e):
    	exitFlagLock.acquire()
    	exitFlag = True
    	exitFlagLock.release()
    	# shutdown make the socket reading thread exit
    	self.sock.shutdown(socket.SHUT_RDWR)
    	self.Destroy()

if __name__ == '__main__':
    app = wx.App()
    WeiqiClient(None, title='*** weiqi client ***')
    app.MainLoop()