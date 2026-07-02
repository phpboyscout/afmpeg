// Backend-B spike: a native libav program whose ENTIRE I/O is custom AVIO
// callbacks over a Unix socket to a Go host serving an in-memory store.
// Remuxes in.mp4 -> out.mp4 (NON-fragmented) — the av_write_trailer moov-patch
// forces backward seeks that a pipe/HTTP-PUT sink cannot do, but seek_cb can.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#define BUFSZ 65536
static int xsend(int fd,const void*p,int n){const char*b=p;int t=0;while(t<n){int k=write(fd,b+t,n-t);if(k<=0)return -1;t+=k;}return 0;}
static int xrecv(int fd,void*p,int n){char*b=p;int t=0;while(t<n){int k=read(fd,b+t,n-t);if(k<=0)return -1;t+=k;}return 0;}
static void p32(uint8_t*b,uint32_t v){for(int i=0;i<4;i++)b[i]=v>>(8*i);}
static uint32_t g32(const uint8_t*b){uint32_t v=0;for(int i=0;i<4;i++)v|=(uint32_t)b[i]<<(8*i);return v;}
static void p64(uint8_t*b,uint64_t v){for(int i=0;i<8;i++)b[i]=v>>(8*i);}
static uint64_t g64(const uint8_t*b){uint64_t v=0;for(int i=0;i<8;i++)v|=(uint64_t)b[i]<<(8*i);return v;}
typedef struct{int fd;}H;
static int connect_open(const char*path,char mode,const char*name){
  int fd=socket(AF_UNIX,SOCK_STREAM,0);
  struct sockaddr_un a;memset(&a,0,sizeof a);a.sun_family=AF_UNIX;strncpy(a.sun_path,path,sizeof(a.sun_path)-1);
  if(connect(fd,(struct sockaddr*)&a,sizeof a)<0){perror("connect");return -1;}
  int nl=strlen(name);uint8_t h[6];h[0]='O';h[1]=mode;p32(h+2,nl);
  xsend(fd,h,6);xsend(fd,name,nl);uint8_t st;xrecv(fd,&st,1);
  if(st){fprintf(stderr,"open %s failed\n",name);return -1;}return fd;
}
static int read_cb(void*o,uint8_t*buf,int sz){H*h=o;uint8_t r[5];r[0]='R';p32(r+1,sz);xsend(h->fd,r,5);
  uint8_t nb[4];if(xrecv(h->fd,nb,4))return AVERROR_EOF;int n=(int)g32(nb);if(n<=0)return AVERROR_EOF;
  if(xrecv(h->fd,buf,n))return AVERROR_EOF;return n;}
static int write_cb(void*o,uint8_t*buf,int sz){H*h=o;uint8_t r[5];r[0]='W';p32(r+1,sz);xsend(h->fd,r,5);xsend(h->fd,buf,sz);
  uint8_t nb[4];xrecv(h->fd,nb,4);return (int)g32(nb);}
static int64_t seek_cb(void*o,int64_t off,int whence){H*h=o;
  if(whence==AVSEEK_SIZE){uint8_t r[1]={'Z'};xsend(h->fd,r,1);uint8_t s[8];xrecv(h->fd,s,8);return (int64_t)g64(s);}
  whence&=~AVSEEK_FORCE;uint8_t r[10];r[0]='S';p64(r+1,(uint64_t)off);r[9]=(uint8_t)whence;xsend(h->fd,r,10);
  uint8_t pb[8];xrecv(h->fd,pb,8);return (int64_t)g64(pb);}
int main(int argc,char**argv){
  const char*sock=argv[1],*inname=argv[2],*outname=argv[3];
  static H hin,hout;hin.fd=connect_open(sock,'r',inname);hout.fd=connect_open(sock,'w',outname);
  if(hin.fd<0||hout.fd<0)return 2;
  AVFormatContext*ifmt=avformat_alloc_context();
  ifmt->pb=avio_alloc_context(av_malloc(BUFSZ),BUFSZ,0,&hin,read_cb,NULL,seek_cb);
  if(avformat_open_input(&ifmt,NULL,NULL,NULL)<0){fprintf(stderr,"open_input\n");return 3;}
  if(avformat_find_stream_info(ifmt,NULL)<0){fprintf(stderr,"stream_info\n");return 3;}
  AVFormatContext*ofmt=NULL;avformat_alloc_output_context2(&ofmt,NULL,"mp4",NULL);
  ofmt->pb=avio_alloc_context(av_malloc(BUFSZ),BUFSZ,1,&hout,NULL,write_cb,seek_cb);
  ofmt->flags|=AVFMT_FLAG_CUSTOM_IO;
  int*map=av_calloc(ifmt->nb_streams,sizeof(int));int oi=0;
  for(unsigned i=0;i<ifmt->nb_streams;i++){AVStream*is=ifmt->streams[i];
    if(is->codecpar->codec_type!=AVMEDIA_TYPE_VIDEO&&is->codecpar->codec_type!=AVMEDIA_TYPE_AUDIO){map[i]=-1;continue;}
    AVStream*os=avformat_new_stream(ofmt,NULL);avcodec_parameters_copy(os->codecpar,is->codecpar);os->codecpar->codec_tag=0;map[i]=oi++;}
  if(avformat_write_header(ofmt,NULL)<0){fprintf(stderr,"write_header\n");return 4;}
  AVPacket*pkt=av_packet_alloc();
  while(av_read_frame(ifmt,pkt)>=0){
    if(map[pkt->stream_index]<0){av_packet_unref(pkt);continue;}
    AVStream*is=ifmt->streams[pkt->stream_index],*os=ofmt->streams[map[pkt->stream_index]];
    pkt->stream_index=map[pkt->stream_index];av_packet_rescale_ts(pkt,is->time_base,os->time_base);pkt->pos=-1;
    av_interleaved_write_frame(ofmt,pkt);av_packet_unref(pkt);}
  av_write_trailer(ofmt);
  fprintf(stderr,"remux OK: %d streams copied\n",oi);
  return 0;
}
