
package tftpwrapper

import (
       "bytes"
       "fmt"
       "io"
       "pack.ag/tftp"
)

const (
       TftpHostPort = "127.0.0.1:4444/"
)

func DownloadFromPath(cli **tftp.Client, path string) []byte {
       buf := new(bytes.Buffer)
       resp, err := (*cli).Get(TftpHostPort + path)
       if err != nil {
               fmt.Println(err)
               return []byte("error")
       }
       io.Copy(buf, resp)
       return buf.Bytes()
}

