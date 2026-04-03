-- 配置中心数据库初始化脚本

CREATE DATABASE IF NOT EXISTS config_center DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE config_center;

-- 用户表
CREATE TABLE IF NOT EXISTS users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 服务表
CREATE TABLE IF NOT EXISTS services (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(50) UNIQUE NOT NULL,
    description VARCHAR(255),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 配置表（不使用物理外键，使用逻辑外键）
CREATE TABLE IF NOT EXISTS configs (
    id INT AUTO_INCREMENT PRIMARY KEY,
    service_id INT NOT NULL,
    env VARCHAR(20) NOT NULL,
    config_key VARCHAR(100) NOT NULL,
    config_value TEXT NOT NULL,
    config_type VARCHAR(20) DEFAULT 'string',
    version INT DEFAULT 1,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_service_env_key (service_id, env, config_key),
    INDEX idx_service_env (service_id, env)
);

-- 服务订阅 Token 表
CREATE TABLE IF NOT EXISTS service_tokens (
    id INT AUTO_INCREMENT PRIMARY KEY,
    service_id INT NOT NULL,
    env VARCHAR(20) NOT NULL,
    token VARCHAR(64) UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_service_env (service_id, env),
    INDEX idx_token (token)
);

-- 插入默认管理员用户 (密码: admin123)
INSERT IGNORE INTO users (username, password) VALUES 
('admin', '$2a$10$8djHZpgU55e0KkCv6vz44.ZVDnaXNy8hCfQfToF/eqF99o7iNhqWe');
