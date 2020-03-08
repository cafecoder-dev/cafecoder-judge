FROM ubuntu:18.04
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
apt install wget -y && \
wget https://dl.google.com/go/go1.14.linux-amd64.tar.gz && \
tar -C /usr/local -xzf go1.14.linux-amd64.tar.gz && \
sed '$a export PATH=$PATH:/usr/local/go/bin' /etc/profile && \
apt install apt-transport-https dirmngr -y
COPY ./executeUsercode .
WORKDIR / 

ENTRYPOINT ["./executeUsercode"]
