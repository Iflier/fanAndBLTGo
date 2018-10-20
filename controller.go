package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tarm/serial"
)

var scanResult bool
var scannerBuffer = make([]byte, 32)
var responsePrefix = []byte("R,2#")
var comReadBuffer = make([]byte, 16)

func main() {
	fmt.Println("[INFO] Starting ...")
	// 配置串口号，波特率，读操作超时时间
	conf := &serial.Config{
		Name:        "COM6",
		Baud:        9600,
		ReadTimeout: 5 * time.Second,
		Size:        8,
	}
	com, err := serial.OpenPort(conf)
	if err != nil {
		fmt.Printf("Failed to open specified port, prepare to exit ...")
		os.Exit(1)
	}
	defer com.Close()
	// 创建一个scanner，用于从终端读取输入的命令，类似于Python语言内置的intput函数
	terminalScanner := bufio.NewScanner(os.Stdin)
	// 设置这个scanner的缓存大小，最多缓存64个字节
	terminalScanner.Buffer(scannerBuffer, 64)
	for {
		fmt.Print("Command -->:")
		scanResult = terminalScanner.Scan()
		checkTerminalScanResult(scanResult)
		fmt.Println("Command from terminal: ", terminalScanner.Text())
		// 如果从终端接收到退出命令字符串，先关闭风机，然后退出
		if strings.Index("exit,quit", strings.ToLower(terminalScanner.Text())) != -1 {
			_, err := com.Write([]byte("N,2#0;"))
			if err != nil {
				fmt.Println("在向串口设备写入数据时发生错误，", err)
			}
			// 需要添加延迟，等待从终端读取的数据写入到串口设备后再关闭设备，然后退出
			// 否则退出后，在下次操作（不重启串口设备）时，第一次发送的命令会导致风机停转
			// 等待时间未精确计量
			time.Sleep(300 * time.Millisecond)
			break
		}
		writtenBytesNum, err := com.Write(connectSlice(responsePrefix, bytes.ToLower(terminalScanner.Bytes())))
		if err != nil {
			fmt.Println("在向串口设备写入数据时发生错误，", err)
		}
		fmt.Printf("向串口设备写入 %v 个字节.\n", writtenBytesNum)
		time.Sleep(600 * time.Millisecond) // 等待串口准备数据
		readBytesNum, err := com.Read(comReadBuffer)
		if err != nil {
			fmt.Println("Failed to read data from serial port, error = ", err)
		}
		fmt.Printf("Response -->:%v\n", string(comReadBuffer[:readBytesNum]))
		// 清空，也不知道清空什么。serial lib中说道：
		// 用于丢弃写入到串口设备中还没有发送的数据或者串口设备已经接收但还没有读取的数据，但是没有接收/发送方向控制
		com.Flush()
	}
	fmt.Println("Done.")
}

func checkTerminalScanResult(result bool) {
	// 检查从终端读取输入返回的状态
	if !scanResult {
		fmt.Println("When scan input from terminal, an error may occured, prepeare to exit ...")
		os.Exit(1)
	}
}

func connectSlice(srcSlice, newSlice []byte) []byte {
	// Go语言文档中明确指出，作为一种特殊情况，可以把字符串附加到字节切片上，但是实际操作却不被允许
	if len(newSlice) == 0 {
		return append(srcSlice, ';')
	}
	for _, elem := range newSlice {
		srcSlice = append(srcSlice, elem)
	}
	return append(srcSlice, ';')
}
