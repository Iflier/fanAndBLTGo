package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shirou/gopsutil/cpu"
	"github.com/tarm/serial"
)

var sleepTime = flag.Int("sleeptime", 1, "Command transmission interval in automatic mode.")
var comPort = flag.String("port", "COM6", "Specify a port for connection(Characteres should be uppper case).")

// 放在这里都是全局可以访问的变量
var scanResult bool
var runMode bool
var avergeSystemUtilization int
var scannerBuffer = make([]byte, 32)
var responsePrefix = []byte("R,2#")
var noResponsePrefix = []byte("N,2#")
var comReadBuffer = make([]byte, 16)
var commandString string
var ch = make(chan bool)
var exitCh = make(chan bool)

// 创建一个scanner，用于从终端读取输入的命令，类似于Python语言内置的intput函数
var terminalScanner = bufio.NewScanner(os.Stdin)

const minDuty int64 = 29

// 配置串口号，波特率，读操作超时时间
var conf = &serial.Config{
	Name:        strings.ToUpper(*comPort),
	Baud:        9600,
	ReadTimeout: 5 * time.Second,
	Size:        8,
}
var com, _ = serial.OpenPort(conf) // 如果不忽略这个函数返回的err会导致语法错误，暂不清楚其原因

func main() {
	flag.Parse()
	defer com.Close()
	fmt.Println("[INFO] Starting ...")
	// 设置这个scanner的缓存大小，最多缓存64个字节。默认大小为 64KB
	terminalScanner.Buffer(scannerBuffer, 64)
	go acceptCommandMode(com, &runMode)
	go autoRunMode(com, &runMode)
	<-exitCh // 在此阻塞等待以上两个goroutine结束，不像Python语言可以设置某些线程为守护线程
	fmt.Println("Done.")
}

func isDigitalStr(inputStr string) bool {
	// 很low的函数 :-(
	strLeng := len(inputStr)
	var count = 0
	for _, val := range inputStr {
		if unicode.IsDigit(rune(val)) {
			count++
		}
	}
	if strLeng == count {
		return true
	} else {
		return false
	}
}

func calculateSpeedToInt64(utilization float64) int64 {
	// CPU利用率与调整风机的占空比之间为线性关系：29 + 0.4*x
	return minDuty + int64(math.Ceil(0.4*utilization))
}

func checkTerminalScanResult(result bool) {
	// 检查从终端读取输入返回的状态
	if !scanResult {
		fmt.Println("When scan input from terminal, an error may occured, prepeare to exit ...")
		os.Exit(1)
	}
}

func acceptCommandMode(comObj *serial.Port, runFlag *bool) {
	for {
		fmt.Print("Command -->:")
		scanResult = terminalScanner.Scan()
		checkTerminalScanResult(scanResult)
		fmt.Println("Command from terminal: ", terminalScanner.Text())
		// 如果从终端接收到退出命令字符串，先关闭风机，然后退出
		commandString = strings.ToLower(terminalScanner.Text())
		if strings.Index("exit,quit", commandString) != -1 {
			_, err := com.Write([]byte("N,2#0;"))
			if err != nil {
				fmt.Println("在向串口设备写入数据时发生错误，", err)
			}
			// 需要添加延迟，等待从终端读取的数据写入到串口设备后再关闭设备，然后退出
			// 否则退出后，在下次操作（不重启串口设备）时，第一次发送的命令会导致风机停转
			// 等待时间未精确计量
			time.Sleep(300 * time.Millisecond)
			*runFlag = false //通知另一个goroutine阻塞，似乎没有啥必要
			exitCh <- true   // 取消主程序（线程？）的阻塞，所有的goroutine都会被结束
			break
		} else if strings.Index("auto", commandString) != -1 {
			// Auto 控制模式
			if *runFlag {
				fmt.Println("Aler in auto run mode.")
			} else {
				fmt.Println("Enter into auto run mode.")
				*runFlag = true
				ch <- true // 另一个goroutine退出阻塞状态
			}
		} else if strings.Index("cancel", commandString) != -1 {
			if !*runFlag {
				fmt.Println("Alerady exit from auto run mode.")
			} else {
				*runFlag = false //通知另一个goroutine退出运行，进入通道阻塞模式
				fmt.Println("Exit from auot run mode.")
			}
		} else if isDigitalStr(commandString) {
			writtenBytesNum, err := com.Write(append(responsePrefix, append(bytes.ToLower(terminalScanner.Bytes()), ";"...)...))
			if err != nil {
				fmt.Println("在向串口设备写入数据时发生错误，", err)
			}
			fmt.Printf("向串口设备写入 %v 个字节.\n", writtenBytesNum)
			time.Sleep(500 * time.Millisecond) // 等待串口准备返回数据
			readBytesNum, err := com.Read(comReadBuffer)
			if err != nil {
				fmt.Println("Failed to read data from serial port, error = ", err)
			}
			fmt.Printf("Response -->:%v\n", string(comReadBuffer[:readBytesNum]))
			// 清空，也不知道清空什么。serial lib中说道：
			// 用于丢弃写入到串口设备中还没有发送的数据或者串口设备已经接收但还没有读取的数据，但是没有接收/发送方向控制
			com.Flush()
		} else {
			fmt.Println("[ERROR] Received an unknown command:", commandString)
		}
	}
}

func autoRunMode(comObj *serial.Port, runFlag *bool) {
	// 这个goroutine只对指针变量 runFlag 有读操作，不涉及修改操作
	// 传递的指针变量是 被引用的 而不是 被复制的
	for {
		if *runFlag {
			avergeSystemUtilization, _ := cpu.Percent(1*time.Second, false)
			utilizationParseToInt64 := calculateSpeedToInt64(avergeSystemUtilization[0])
			if 0 <= utilizationParseToInt64 && utilizationParseToInt64 <= 100 {
				// 不打算接收函数的任何返回值，接收的话提示没有新值的语法错误，因为终端被用于接收command，不能输出
				comObj.Write(append(noResponsePrefix, append([]byte(strconv.FormatInt(utilizationParseToInt64, 10)), ";"...)...))
			} else {
				// 如果能运行到这里，很可能是发生了什么错误了，为避免风机失控，选择关闭它
				comObj.Write(append(noResponsePrefix, append([]byte("0"), ";"...)...))
			}
		} else {
			// 如果 runFlag 被修改为false，回到这里阻塞
			<-ch
		}
	}
}
