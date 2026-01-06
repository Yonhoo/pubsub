-- 初始化数据库脚本
-- 创建 rooms 表
CREATE TABLE IF NOT EXISTS rooms (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    description TEXT,
    max_users INT DEFAULT 100,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL,
    INDEX idx_created_at (created_at),
    INDEX idx_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 创建 room_users 表
CREATE TABLE IF NOT EXISTS room_users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    user_name VARCHAR(128) NOT NULL,
    room_id VARCHAR(64) NOT NULL,
    node_id VARCHAR(64) NOT NULL,
    joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    left_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    INDEX idx_user_room (user_id, room_id),
    INDEX idx_room_id (room_id),
    INDEX idx_user_id (user_id),
    INDEX idx_node_id (node_id),
    INDEX idx_joined_at (joined_at),
    INDEX idx_left_at (left_at),
    INDEX idx_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 创建 connect_nodes 表
CREATE TABLE IF NOT EXISTS connect_nodes (
    id VARCHAR(64) PRIMARY KEY,
    address VARCHAR(256) NOT NULL,
    region VARCHAR(64),
    max_connections INT DEFAULT 10000,
    current_connections INT DEFAULT 0,
    cpu_usage FLOAT DEFAULT 0,
    memory_usage FLOAT DEFAULT 0,
    status VARCHAR(32) DEFAULT 'online',
    last_heartbeat TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_last_heartbeat (last_heartbeat),
    INDEX idx_current_connections (current_connections)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


