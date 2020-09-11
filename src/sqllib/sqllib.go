package sqllib

import (
	"fmt"
	"os"

	// mysql driver
	_ "github.com/go-sql-driver/mysql"

	"github.com/jinzhu/gorm"
	"github.com/joho/godotenv"
)

// NewDB ... db の client を返す
func NewDB() (database *gorm.DB, err error) {
	if err = godotenv.Load("./.env"); err != nil {
		return nil, err
	}

	DBMS := os.Getenv("DBMS")
	DBNAME := os.Getenv("DB_NAME")
	USER := os.Getenv("DB_USER")
	PASS := os.Getenv("DB_PASS")
	HOST := os.Getenv("DB_HOST")
	PORT := os.Getenv("DB_PORT")

	PROTOCOL := fmt.Sprintf("tcp(%s:%s)", HOST, PORT)

	CONNECT := USER + ":" + PASS + "@" + PROTOCOL + "/" + DBNAME + "?charset=utf8&parseTime=true&loc=Asia%2FTokyo"

	return gorm.Open(DBMS, CONNECT)
}
