CREATE TABLE users (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(254) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    created_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE teams (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_by BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    task_revision BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE team_members (
    team_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role ENUM('owner', 'admin', 'member') NOT NULL,
    PRIMARY KEY (team_id, user_id),
    INDEX idx_members_user (user_id, team_id),
    FOREIGN KEY (team_id) REFERENCES teams(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE tasks (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    team_id BIGINT NOT NULL,
    title VARCHAR(200) NOT NULL,
    description TEXT NOT NULL,
    status ENUM('todo', 'in_progress', 'done') NOT NULL,
    created_by BIGINT NOT NULL,
    assignee_id BIGINT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    closed_at DATETIME(6) NULL,
    version BIGINT NOT NULL DEFAULT 1,
    INDEX idx_tasks_team (team_id, id),
    INDEX idx_tasks_status (team_id, status, id),
    INDEX idx_tasks_assignee (team_id, assignee_id, id),
    INDEX idx_tasks_closed (team_id, status, closed_at, assignee_id),
    FOREIGN KEY (team_id) REFERENCES teams(id),
    FOREIGN KEY (team_id, created_by) REFERENCES team_members(team_id, user_id),
    FOREIGN KEY (team_id, assignee_id) REFERENCES team_members(team_id, user_id),
    CONSTRAINT chk_task_version CHECK (version >= 1),
    CONSTRAINT chk_task_closed CHECK ((status = 'done' AND closed_at IS NOT NULL) OR (status <> 'done' AND closed_at IS NULL))
) ENGINE=InnoDB;

CREATE TABLE task_history (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id BIGINT NOT NULL,
    changed_by BIGINT NOT NULL,
    changes JSON NOT NULL,
    created_at DATETIME(6) NOT NULL,
    INDEX idx_history_task (task_id, id),
    FOREIGN KEY (task_id) REFERENCES tasks(id),
    FOREIGN KEY (changed_by) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE task_comments (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    INDEX idx_comments_task (task_id, id),
    FOREIGN KEY (task_id) REFERENCES tasks(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;
