FROM ubuntu:latest
#install compilers
RUN \
apt update && \
apt-get install ca-certificates -y && \
apt install -y wget gnupg &&\
yes | apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 3FA7E0328081BFF6A14DA29AA6A19B38D3D831EF && \
echo "deb https://download.mono-project.com/repo/ubuntu vs-bionic main" | tee /etc/apt/sources.list.d/mono-official-vs.list && \
apt update && \
apt-get install monodevelop -y && \
apt update && \
apt install gcc -y && \
apt install g++ -y  && \
apt install default-jdk -y && \
apt install python3 -y && \
apt install apt-transport-https dirmngr -y && \
useradd -m rbash_user && \
mkdir cafecoderUsers && \
chown rbash_user:rbash_user cafecoderUsers

COPY executeUsercode.sh .

ENTRYPOINT ["/bin/sh","-c","while :; do sleep 10; done" ]
