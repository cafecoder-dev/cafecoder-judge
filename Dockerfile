FROM ubuntu:18.04
#install compilers
RUN \
apt update && \
apt-get install software-properties-common apt-transport-https dirmngr -y && \
apt install curl wget -y && \
# C#(mono) install
apt install -y wget gnupg ca-certificates && \
yes | apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 3FA7E0328081BFF6A14DA29AA6A19B38D3D831EF && \
echo "deb https://download.mono-project.com/repo/ubuntu vs-bionic main" | tee /etc/apt/sources.list.d/mono-official-vs.list && \
apt update && \
apt-get install monodevelop -y && \
# C#(.NET) install
wget -q https://packages.microsoft.com/config/ubuntu/18.04/packages-microsoft-prod.deb -O packages-microsoft-prod.deb && \
dpkg -i packages-microsoft-prod.deb && \
add-apt-repository universe && \
apt-get update && \
apt-get install apt-transport-https -y && \
apt-get update && \
apt-get install dotnet-sdk-3.1 -y && \
# C/C++ install
add-apt-repository ppa:ubuntu-toolchain-r/test && \
apt-get update && \
apt-get install g++-9-multilib -y && \
# Java11 install
apt install default-jdk -y && \
# Python3 install
apt install python3 -y && \
# go install
wget https://dl.google.com/go/go1.14.linux-amd64.tar.gz && \
tar -C /usr/local -xzf go1.14.linux-amd64.tar.gz && \
export PATH=$PATH:/usr/local/go/bin &&  \
# Rust install
curl https://sh.rustup.rs -sSf | sh -s -- -y

COPY ./executeUsercode .
WORKDIR / 

ENTRYPOINT ["./executeUsercode"]
