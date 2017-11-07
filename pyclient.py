import socket
import struct
import wx
from wx.lib.pubsub import pub
from threading import Thread
from socketmsg import SocketReader
import wq_pb2 as wq

class ThreadSocketReader(Thread,SocketReader):
    def __init__(self,socket):
        Thread.__init__(self)
        SocketReader.__init__(self,socket)
    def run(self):
        self.readMessage()
    def processMessage(self,bin):
        print("---------------")
        s = wq.Msg()
        s.ParseFromString(bin)
        pub.sendMessage('message_topic',message=s)

# Bind(event, handler, source=None, id=wx.ID_ANY, id2=wx.ID_ANY)
class Example(wx.Frame):
  
    def __init__(self, parent, title):
        super(Example, self).__init__(parent, title=title, size=(600, 400))
        self.InitUI()
        self.Centre()
        self.Show()
    def InitUI(self):
        panel = wx.Panel(self)
        panel.SetBackgroundColour('#2F4F2F')
        vbox = wx.BoxSizer(wx.VERTICAL)

        self.connectBt = wx.Button(panel,wx.ID_ANY,'Connect')
        self.sendMsgBt = wx.Button(panel,wx.ID_ANY,'Send Message')
        self.Bind(wx.EVT_BUTTON,self.OnConnectBt,source=self.connectBt)
        self.Bind(wx.EVT_BUTTON,self.OnSendMsgBt,source=self.sendMsgBt)
        vbox.Add(self.connectBt, 1, wx.EXPAND)
        vbox.Add(self.sendMsgBt, 1, wx.EXPAND)
        panel.SetSizer(vbox)

        pub.subscribe(self.receivedMessage,"message_topic")
    def OnConnectBt(self,e):
        self.connect()
    def OnSendMsgBt(self,e):
        msg = wq.Msg()
        msg.login.pid = "wenjixiao"
        msg.login.passwd = "123456"
        bin = msg.SerializeToString()
        self.send(bin)
    def receivedMessage(self,message):
        print "...received msg..."
        print(message)
    def send(self,bin):
        header = struct.pack('I',len(bin))
        self.sock.sendall(header+bin)
    def connect(self):
        self.addr = ('127.0.0.1',5678)
        self.sock = socket.socket(socket.AF_INET,socket.SOCK_STREAM)
        self.sock.connect(self.addr)
        ThreadSocketReader(self.sock).start()

if __name__ == '__main__':
  
    app = wx.App()
    Example(None, title='Border')
    app.MainLoop()