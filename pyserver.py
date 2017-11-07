import struct
from gevent.server import StreamServer
from socketmsg import SocketReader
import student_pb2 as stu

class ServerSocketReader(SocketReader):
	def __init__(self,socket):
		SocketReader.__init__(self,socket)
	def processMessage(self,bin):
		print("---------------")
		s = stu.Student()
		s.ParseFromString(bin)
		print s
		s.age = 999
		self.send(s.SerializeToString())

def handle(socket,address):
	print "address =",address
	ServerSocketReader(socket).readMessage()

server = StreamServer(('127.0.0.1',5678),handle)
server.serve_forever()