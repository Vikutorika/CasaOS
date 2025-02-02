package service

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	httper2 "github.com/IceWhaleTech/CasaOS/pkg/utils/httper"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/IceWhaleTech/CasaOS/types"
	"github.com/lucas-clemente/quic-go"
	"gorm.io/gorm"
)

type PersonService interface {
	GetPersionInfo(token string) (m model.PersionModel, err error)
	Handshake(m model.ConnectState)
	Download(m model.MessageModel)
	GetFileDetail(uuid, path, to string)
	SendFileData(m model.MessageModel, blockSize int, length int)
	ReplyGetFileDetail(m model.MessageModel)
	ReceiveFileData(m model.MessageModel)
	ReceiveGetFileDetail(m model.MessageModel)

	//------------ database
	AddDownloadTask(m model2.PersionDownloadDBModel)   //添加下载任务
	EditDownloadState(m model2.PersionDownloadDBModel) //只修改状态
	EditDownloading(m model2.PersionDownloadDBModel, section model2.PersionFileSectionModel)
	SaveDownloadState(m model2.PersionDownloadDBModel)
	DelDownload(uuid string)
	GetDownloadById(uuid string) model2.PersionDownloadDBModel
}

type personService struct {
	db *gorm.DB
}

var IpInfo model.PersionModel

func PushIpInfo(token string) {

	m := model.PersionModel{}
	m.Ips = GetDeviceAllIP()
	m.Token = token
	b, _ := json.Marshal(m)

	if reflect.DeepEqual(IpInfo, m) {
		return
	}
	head := make(map[string]string)
	infoS := httper2.Post(config.ServerInfo.Handshake+"/v1/update", b, "application/json", head)
	fmt.Println(infoS)
}
func (p *personService) GetPersionInfo(token string) (m model.PersionModel, err error) {
	infoS := httper2.Get(config.ServerInfo.Handshake+"/v1/ips/"+token, nil)
	err = json.Unmarshal([]byte(infoS), &m)
	return
}

//尝试连接
func (p *personService) Handshake(m model.ConnectState) {
	//1先进行udp打通成功

	srcAddr := &net.UDPAddr{
		IP: net.IPv4zero, Port: 9901} //注意端口必须固定
	dstAddr := &net.UDPAddr{
		IP: net.ParseIP(config.ServerInfo.Handshake), Port: 9527}
	//DialTCP在网络协议net上连接本地地址laddr和远端地址raddr。net必须是"udp"、"udp4"、"udp6"；如果laddr不是nil，将使用它作为本地地址，否则自动选择一个本地地址。
	//(conn)UDPConn代表一个UDP网络连接，实现了Conn和PacketConn接口
	conn, err := net.DialUDP("udp", srcAddr, dstAddr)
	if err != nil {
		fmt.Println(err)
	}
	b, _ := json.Marshal(m)
	if _, err = conn.Write(b); err != nil {
		fmt.Println(err)
	}
	data := make([]byte, 1024)
	//ReadFromUDP从c读取一个UDP数据包，将有效负载拷贝到b，返回拷贝字节数和数据包来源地址。
	//ReadFromUDP方***在超过一个固定的时间点之后超时，并返回一个错误。
	n, _, err := conn.ReadFromUDP(data)
	if err != nil {
		fmt.Printf("error during read: %s", err)
	}
	conn.Close()
	toPersion := model.PersionModel{}
	err = json.Unmarshal(data[:n], &toPersion)
	if err != nil {
		fmt.Println(err)
	}

	//websocket 连接
	// bidirectionHole(srcAddr, &anotherPeer)

	//2udp打洞成功向服务器汇报打洞结果
	//3转udp打洞

}

func (p *personService) AddDownloadTask(m model2.PersionDownloadDBModel) {
	p.db.Create(&m)
}
func (p *personService) EditDownloadState(m model2.PersionDownloadDBModel) {
	p.db.Model(&m).Where("uuid = ?", m.UUID).Update("state", m.State)
}

func (p *personService) EditDownloading(m model2.PersionDownloadDBModel, section model2.PersionFileSectionModel) {
	b, _ := json.Marshal(section)
	m.Section = string(b)
	p.db.Model(&m).Where("uuid = ?", m.UUID).Update("section", m.Section)
}

func (p *personService) DelDownload(uuid string) {
	var m model2.PersionDownloadDBModel
	p.db.Where("uuid = ?", uuid).Delete(&m)
}
func (p *personService) GetDownloadById(uuid string) model2.PersionDownloadDBModel {
	var m model2.PersionDownloadDBModel
	p.db.Model(m).Where("uuid = ?", uuid).First(&m)
	return m
}

func (p *personService) SaveDownloadState(m model2.PersionDownloadDBModel) {
	p.db.Save(&m)
}

var ipAddress chan string

type sysConn struct {
	conn   *net.UDPConn
	header string
	auth   cipher.AEAD
}

func UDPConnect(ips []string) {
	quicConfig := &quic.Config{
		ConnectionIDLength:    12,
		HandshakeIdleTimeout:  time.Second * 8,
		MaxIdleTimeout:        time.Second * 45,
		MaxIncomingStreams:    32,
		MaxIncomingUniStreams: -1,
		KeepAlive:             true,
	}
	fmt.Println(quicConfig)
	//PersonUDPMap = make(map[string]*net.UDPAddr)
	ipAddress = make(chan string)

	srcAddr := &net.UDPAddr{
		IP: net.IPv4zero, Port: 9901}
	fmt.Println(srcAddr)
	//UDPconn, err := net.ListenUDP("udp", srcAddr)
	// sysconn := &sysConn{
	// 	conn:   UDPconn,
	// 	header: "",
	// 	auth:   nil,
	// }
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// liste, err := quic.Listen(UDPconn, generateTLSConfig(), nil)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// ssss, err := liste.Accept(context.Background())
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// st, err := ssss.AcceptStream(context.Background())
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// st.Write([]byte("ssss"))
	qlister, err := quic.ListenAddr("0.0.0.0:9901", generateTLSConfig(), nil)
	//qlister, err := quic.Listen(UDPconn, nil, nil)
	if err != nil {
		fmt.Println("quic错误", qlister)
	}
	//session, e := qlister.Accept()
	sess, err := qlister.Accept(context.Background())
	sess.SendMessage([]byte("aaaa"))
	stream, err := sess.AcceptStream(context.Background())
	stream.Write([]byte("bbb"))
	//quic.Dial()
	if err != nil {
		fmt.Println("quic错误", qlister)
	}

	if err != nil {
		fmt.Println("监听错误", err.Error())
	}
	for _, v := range ips {
		dstAddr := &net.UDPAddr{
			IP: net.ParseIP(v), Port: 9901}

		fmt.Println(v, "开始监听")

		//quic.Dial()

		go AsyncUDPConnect(dstAddr)
	}

	for {
		data := make([]byte, 1024)
		n, add, err := UDPconn.ReadFromUDP(data)
		fmt.Println(add)
		if err != nil {
			log.Printf("error during read:%s\n", err)
		} else {

			fmt.Println("收到数据：", string(data[:n]))
			msg := model.MessageModel{}
			err := json.Unmarshal(data[:n], &msg)
			if err != nil {
				log.Printf("转义错误:%s\n", err)
			}
			//todo:检查数据库是否为合法请求
			if msg.Type == "hi" {
				//add ip
				//PersonUDPMap[msg.From] = add
			} else if msg.Type == "browse" {
				//获取目录结构
			} else if msg.Type == "file_detail" {
				MyService.Person().ReplyGetFileDetail(msg)
			} else if msg.Type == "file_detail_reply" {
				MyService.Person().ReceiveGetFileDetail(msg)
			} else if msg.Type == "file_data_reply" {
				MyService.Person().ReceiveFileData(msg)
			} else {
				fmt.Println("未知事件")
			}

		}
	}
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	// return &tls.Config{
	// 	ClientSessionCache:     globalSessionCache,
	// 	RootCAs:                root,
	// 	InsecureSkipVerify:     false,
	// 	NextProtos:             nil,
	// 	SessionTicketsDisabled: true,
	// }
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}
}

//首次获取文件信息
func (p *personService) GetFileList(uuid, path, to string) {

	msg := model.MessageModel{}
	msg.Type = "file_list"
	msg.Data = path
	msg.To = to
	msg.From = config.ServerInfo.Token
	msg.UUId = uuid
	b, _ := json.Marshal(msg)
	fmt.Println(b)
	// if ip, ok := PersonUDPMap[msg.To]; ok {
	// 	_, err := UDPconn.WriteToUDP(b, ip)
	// 	if err != nil {
	// 		fmt.Println("写入错误", err)
	// 	}
	// }
	//接收

}

//首次获取文件信息
func (p *personService) GetFileDetail(uuid, path, to string) {

	msg := model.MessageModel{}
	msg.Type = "file_detail"
	msg.Data = path
	msg.To = to
	msg.From = config.ServerInfo.Token
	msg.UUId = uuid
	b, _ := json.Marshal(msg)
	fmt.Println(b)
	// if ip, ok := PersonUDPMap[msg.To]; ok {
	// 	_, err := UDPconn.WriteToUDP(b, ip)
	// 	if err != nil {
	// 		fmt.Println("写入错误", err)
	// 	}
	// }
	//创建临时文件夹
	file.MkDir("/oasis/download/" + uuid)
}

func (p *personService) Download(m model.MessageModel) {
	fDetail, err := os.Stat("/Users/liangjianli/Documents/images")
	//发送需要发送的数据摘要
	if err != nil {
		fmt.Println("未获取到文件信息")
	}
	summary := model.FileSummaryModel{}
	summary.Hash = file.GetHashByPath(fDetail.Name())
	summary.Path = m.Data.(string)
	summary.BlockSize, summary.Length = file.GetBlockInfo(fDetail.Size())

	msg := model.MessageModel{}
	msg.Type = "download-reply"
	msg.Data = summary
	msg.From = config.ServerInfo.Token
	msg.UUId = ""
	b, _ := json.Marshal(msg)

	fmt.Println(b)

	// if ip, ok := PersonUDPMap[m.From]; ok {
	// 	_, err := UDPconn.WriteToUDP(b, ip)
	// 	if err != nil {
	// 		fmt.Println("写入错误", err)
	// 	}
	// }
}

//receive file data
func (p *personService) ReceiveFileData(m model.MessageModel) {
	task := p.GetDownloadById(m.UUId)

	//需要重置参数
	tempPath := "/oasis/download/" + task.UUID
	tempFilePath := tempPath + "/" + task.Name
	fmt.Println(tempFilePath)
	filePath := "/oasis/download/" + task.Name

	bss, _ := json.Marshal(m.Data)
	tran := model.TranFileModel{}
	err := json.Unmarshal(bss, &tran)
	if err != nil {
		fmt.Println(err)
	}
	// if file.ComparisonHash(tran.Hash) {
	// 	f, err := os.Create(tempFilePath + strconv.Itoa(tran.Index))
	// 	if err != nil {
	// 		fmt.Println("创建文件错误", err)
	// 	}
	// 	defer f.Close()
	// 	//		_, err = f.Write(tran.Data)
	// 	if err != nil {
	// 		fmt.Println("写入错误", err, tran.Index)
	// 	}
	// }
	var k int
	err = filepath.Walk(tempPath, func(filename string, fi os.FileInfo, err error) error { //遍历目录
		if fi.IsDir() { // 忽略目录
			return nil
		}
		k++
		return nil
	})
	if err != nil {
		fmt.Println("获取文件错误", err)
	}
	if task.Length == k {
		//err := file.SpliceFiles(tempPath, filePath)
		if err == nil {
			if h := file.GetHashByPath(filePath); h == task.Hash {
				//最终文件比对成功
				task.State = types.DOWNLOADFINISH
				p.EditDownloadState(task)
				//remove temp path
				file.RMDir(tempPath)
			}
		}
	}

}

//1:say hi
//2:发送文件名称
//3:发送数据

//========================================接收端============================================================================================

// reply file detail
func (p *personService) ReplyGetFileDetail(m model.MessageModel) {
	path := m.Data.(string)
	f, err := os.Stat(path)
	if err != nil {
		fmt.Println(err)
	}
	summary := model.FileSummaryModel{}
	summary.Name = f.Name()
	summary.Size = f.Size()
	summary.Hash = file.GetHashByPath(path)
	summary.Path = path
	summary.BlockSize, summary.Length = file.GetBlockInfo(f.Size())

	msg := model.MessageModel{}
	msg.Type = "file_detail_reply"
	msg.Data = summary
	msg.From = config.ServerInfo.Token
	msg.To = m.From
	msg.UUId = m.UUId
	b, _ := json.Marshal(msg)
	// if ip, ok := PersonUDPMap[m.To]; ok {
	// 	_, err := UDPconn.WriteToUDP(b, ip)
	// 	if err != nil {
	// 		fmt.Println("写入错误", err)
	// 	}
	// }
	fmt.Println(b)
	//开始发送数据
	p.SendFileData(m, summary.BlockSize, summary.Length)
}

func (p *personService) SendFileData(m model.MessageModel, blockSize int, length int) {
	path := m.Data.(string)

	f, err := os.Open(path)
	if err != nil {
		//读取时移动了文件,需要保存数据到数据库
		fmt.Println("读取失败", err)
	}
	buf := make([]byte, blockSize)
	for i := 0; i < length; i++ {
		tran := model.TranFileModel{}
		_, err := f.Read(buf)
		if err == io.EOF {
			fmt.Println("读取完毕", err)
		}
		tran.Hash = file.GetHashByContent(buf)
		tran.Index = i + 1

		msg := model.MessageModel{}
		msg.Type = "file_data_reply"
		msg.Data = tran
		msg.From = config.ServerInfo.Token
		msg.To = m.From
		msg.UUId = m.UUId
		b, _ := json.Marshal(msg)
		// if ip, ok := PersonUDPMap[m.To]; ok {
		// 	_, err := UDPconn.WriteToUDP(b, ip)
		// 	if err != nil {
		// 		fmt.Println("写入错误", err)
		// 	}
		// }
		fmt.Println(b)
	}

}

// 文件摘要返回
func (p *personService) ReceiveGetFileDetail(m model.MessageModel) {

	task := p.GetDownloadById("")
	bss, _ := json.Marshal(m.Data)
	summary := model.FileSummaryModel{}
	err := json.Unmarshal(bss, &summary)
	if err != nil {
		fmt.Println(err)
	}
	task.Hash = summary.Hash
	task.Length = summary.Length
	task.Size = summary.Size

	p.SaveDownloadState(task)
}

func AsyncUDPConnect(dst *net.UDPAddr) {
	for {
		time.Sleep(2 * time.Second)
		if _, err := UDPconn.WriteToUDP([]byte(dst.IP.String()+" is ok"), dst); err != nil {
			log.Println("send msg fail", err)
			return
		} else {
			fmt.Println(dst.IP)
			fmt.Println(dst.IP.To4())
		}
	}
}
func NewPersonService(db *gorm.DB) PersonService {
	return &personService{db: db}
}
