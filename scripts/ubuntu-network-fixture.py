#!/usr/bin/env python3
"""Offline namespace fixture. Contains no subscriptions or external traffic."""
import socketserver
import struct
import threading


class DNS(socketserver.BaseRequestHandler):
    def handle(self):
        query, sock = self.request
        pos = 12
        while pos < len(query) and query[pos]:
            pos += query[pos] + 1
        end = pos + 5
        question = query[12:end]
        qtype = struct.unpack('!H', query[pos + 1:pos + 3])[0]
        answer = b'\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x3c\x00\x04\xcb\x00\x71\x0a' if qtype == 1 else b''
        response = query[:2] + b'\x81\x80' + struct.pack('!HHHH', 1, bool(answer), 0, 0) + question + answer
        sock.sendto(response, self.client_address)


class HTTP(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(5)
        data = b''
        while b'\r\n\r\n' not in data and len(data) < 16384:
            part = self.request.recv(4096)
            if not part:
                return
            data += part
        if data.startswith(b'CONNECT '):
            self.request.sendall(b'HTTP/1.1 200 Connection Established\r\n\r\n')
            data = b''
            while b'\r\n\r\n' not in data and len(data) < 16384:
                part = self.request.recv(4096)
                if not part:
                    return
                data += part
        body = self.server.label.encode()
        self.request.sendall(b'HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body)


class TCP(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


for port, label in [(80, 'via-direct'), (3128, 'via-A'), (3129, 'via-B')]:
    server = TCP(('0.0.0.0', port), HTTP)
    server.label = label
    threading.Thread(target=server.serve_forever, daemon=True).start()
socketserver.ThreadingUDPServer(('0.0.0.0', 5353), DNS).serve_forever()
