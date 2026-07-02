FROM golang:1.24-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc pkg-config ffmpeg libavformat-dev libavcodec-dev libavutil-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /spike
COPY avio_spike.c host.go ./
RUN gcc -O2 -Wall avio_spike.c -o avio_spike $(pkg-config --cflags --libs libavformat libavcodec libavutil)
RUN go build -o host host.go
CMD ["./host"]
