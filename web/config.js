// 配置文件 - 根据环境自动切换
const CONFIG = {
    // WebSocket 连接地址 (Connect-Node)
    // 注意：在 Docker 环境中，即使通过 localhost 访问，WebSocket 也应该连接到 localhost:8083
    // 因为端口映射在宿主机上，浏览器直接连接到宿主机端口
    WS_URL: (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1')
        ? 'ws://localhost:8083/connect'  // 本地开发或 Docker 通过 localhost 访问
        : `ws://${window.location.hostname}:8083/connect`,  // 通过 IP 或其他域名访问
    
    // HTTP API 地址 (Web-Server，与页面在同一服务)
    API_URL: (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1')
        ? 'http://localhost:8086'  // 本地开发或 Docker 通过 localhost 访问
        : `http://${window.location.hostname}:8086`,  // 通过 IP 或其他域名访问
};

console.log('📝 当前配置:', CONFIG);

