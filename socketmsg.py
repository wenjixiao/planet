import socket
import struct

class SocketReader(object):
    def __init__(self,socket):
        self.socketBufSize = 1024
        self.socket = socket
        self.buf = bytes()
        self.headSize = 4
    def readMessage(self):
        print("start read socket...")
        try:
            while True:
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
        finally:
            self.socket.close()
    def send(self,bin):
        header = struct.pack('I',len(bin))
        self.socket.sendall(header+bin)
    def processMessage(self,bin):
        print "***socket reader process message***"
