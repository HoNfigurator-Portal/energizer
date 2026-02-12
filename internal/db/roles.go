package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// RolesDatabase manages the RBAC system using SQLite.
// It replaces the Python RolesDatabase (~595 lines) with a cleaner,
// type-safe Go implementation.
type RolesDatabase struct {
	db *Database
}

// User represents a registered user in the RBAC system.
type User struct {
	ID        int       `json:"id"`
	DiscordID string    `json:"discord_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	Roles     []string  `json:"roles"`
}

// Role represents a role in the RBAC system.
type Role struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Inherits    string   `json:"inherits,omitempty"`
}

// NewRolesDatabase creates and initializes the roles database.
func NewRolesDatabase(dbPath string) (*RolesDatabase, error) {
	database, err := NewDatabase(dbPath)
	if err != nil {
		return nil, err
	}

	rdb := &RolesDatabase{db: database}

	// Run migrations
	if err := rdb.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate roles database: %w", err)
	}

	// Seed default roles
	if err := rdb.seedDefaults(); err != nil {
		return nil, fmt.Errorf("failed to seed default roles: %w", err)
	}

	return rdb, nil
}

// migrate creates the database schema.
func (rdb *RolesDatabase) migrate() error {
	schema := `
		CREATE TABLE IF NOT EXISTS roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			inherits TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		);

		CREATE TABLE IF NOT EXISTS role_permissions (
			role_id INTEGER NOT NULL,
			permission_id INTEGER NOT NULL,
			PRIMARY KEY (role_id, permission_id),
			FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
			FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			discord_id TEXT UNIQUE NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS user_roles (
			user_id INTEGER NOT NULL,
			role_id INTEGER NOT NULL,
			PRIMARY KEY (user_id, role_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			acknowledged INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_users_discord_id ON users(discord_id);
		CREATE INDEX IF NOT EXISTS idx_alerts_type ON alerts(type);
		CREATE INDEX IF NOT EXISTS idx_alerts_acknowledged ON alerts(acknowledged);
	`

	_, err := rdb.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}

	log.Debug().Msg("database schema migrated")
	return nil
}

// seedDefaults creates the default roles and permissions if they don't exist.
func (rdb *RolesDatabase) seedDefaults() error {
	return rdb.db.Transaction(func(tx *sql.Tx) error {
		// Seed permissions
		permissions := []string{"monitor", "control", "configure"}
		for _, perm := range permissions {
			_, err := tx.Exec(
				"INSERT OR IGNORE INTO permissions (name) VALUES (?)", perm)
			if err != nil {
				return err
			}
		}

		// Seed roles with permission mappings
		roles := []struct {
			name     string
			perms    []string
			inherits string
		}{
			{name: "user", perms: []string{"monitor"}, inherits: ""},
			{name: "admin", perms: []string{"monitor", "control"}, inherits: "user"},
			{name: "superadmin", perms: []string{"monitor", "control", "configure"}, inherits: "admin"},
		}

		for _, role := range roles {
			res, err := tx.Exec(
				"INSERT OR IGNORE INTO roles (name, inherits) VALUES (?, ?)",
				role.name, role.inherits)
			if err != nil {
				return err
			}

			// Get role ID
			var roleID int64
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected > 0 {
				roleID, _ = res.LastInsertId()
			} else {
				row := tx.QueryRow("SELECT id FROM roles WHERE name = ?", role.name)
				row.Scan(&roleID)
			}

			// Assign permissions to role
			for _, perm := range role.perms {
				var permID int64
				row := tx.QueryRow("SELECT id FROM permissions WHERE name = ?", perm)
				if err := row.Scan(&permID); err != nil {
					continue
				}
				tx.Exec(
					"INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)",
					roleID, permID)
			}
		}

		return nil
	})
}

// UserHasPermission checks if a user (by Discord ID) has a specific permission.
func (rdb *RolesDatabase) UserHasPermission(discordID, permission string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM users u
		JOIN user_roles ur ON u.id = ur.user_id
		JOIN role_permissions rp ON ur.role_id = rp.role_id
		JOIN permissions p ON rp.permission_id = p.id
		WHERE u.discord_id = ? AND p.name = ?
	`

	var count int
	err := rdb.db.QueryRow(query, discordID, permission).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("permission check failed: %w", err)
	}

	return count > 0, nil
}

// GetAllUsers returns all registered users with their roles.
func (rdb *RolesDatabase) GetAllUsers() ([]User, error) {
	query := `
		SELECT u.id, u.discord_id, u.username, u.created_at
		FROM users u
		ORDER BY u.created_at
	`

	rows, err := rdb.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DiscordID, &u.Username, &u.CreatedAt); err != nil {
			continue
		}

		// Get roles for this user
		roleRows, err := rdb.db.Query(`
			SELECT r.name FROM roles r
			JOIN user_roles ur ON r.id = ur.role_id
			JOIN users u ON u.id = ur.user_id
			WHERE u.discord_id = ?
		`, u.DiscordID)
		if err == nil {
			for roleRows.Next() {
				var roleName string
				roleRows.Scan(&roleName)
				u.Roles = append(u.Roles, roleName)
			}
			roleRows.Close()
		}

		users = append(users, u)
	}

	return users, nil
}

// CreateUser creates a new user and assigns an initial role.
func (rdb *RolesDatabase) CreateUser(discordID, username, role string) error {
	return rdb.db.Transaction(func(tx *sql.Tx) error {
		// Create user
		res, err := tx.Exec(
			"INSERT INTO users (discord_id, username) VALUES (?, ?)",
			discordID, username)
		if err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}

		userID, _ := res.LastInsertId()

		// Get role ID
		var roleID int64
		err = tx.QueryRow("SELECT id FROM roles WHERE name = ?", role).Scan(&roleID)
		if err != nil {
			return fmt.Errorf("role '%s' not found: %w", role, err)
		}

		// Assign role
		_, err = tx.Exec(
			"INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)",
			userID, roleID)
		if err != nil {
			return fmt.Errorf("failed to assign role: %w", err)
		}

		log.Info().
			Str("discord_id", discordID).
			Str("username", username).
			Str("role", role).
			Msg("user created")

		return nil
	})
}

// DeleteUser removes a user by Discord ID.
func (rdb *RolesDatabase) DeleteUser(discordID string) error {
	_, err := rdb.db.Exec("DELETE FROM users WHERE discord_id = ?", discordID)
	return err
}

// AssignRole assigns a role to a user.
func (rdb *RolesDatabase) AssignRole(discordID, role string) error {
	return rdb.db.Transaction(func(tx *sql.Tx) error {
		var userID, roleID int64

		err := tx.QueryRow("SELECT id FROM users WHERE discord_id = ?", discordID).Scan(&userID)
		if err != nil {
			return fmt.Errorf("user not found: %w", err)
		}

		err = tx.QueryRow("SELECT id FROM roles WHERE name = ?", role).Scan(&roleID)
		if err != nil {
			return fmt.Errorf("role not found: %w", err)
		}

		_, err = tx.Exec(
			"INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)",
			userID, roleID)
		return err
	})
}

// RemoveRole removes a role from a user.
func (rdb *RolesDatabase) RemoveRole(discordID, role string) error {
	_, err := rdb.db.Exec(`
		DELETE FROM user_roles
		WHERE user_id = (SELECT id FROM users WHERE discord_id = ?)
		AND role_id = (SELECT id FROM roles WHERE name = ?)
	`, discordID, role)
	return err
}

// GetAllRoles returns all available roles with their permissions.
func (rdb *RolesDatabase) GetAllRoles() ([]Role, error) {
	rows, err := rdb.db.Query("SELECT id, name, inherits FROM roles ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Inherits); err != nil {
			continue
		}

		// Get permissions
		permRows, err := rdb.db.Query(`
			SELECT p.name FROM permissions p
			JOIN role_permissions rp ON p.id = rp.permission_id
			WHERE rp.role_id = ?
		`, r.ID)
		if err == nil {
			for permRows.Next() {
				var perm string
				permRows.Scan(&perm)
				r.Permissions = append(r.Permissions, perm)
			}
			permRows.Close()
		}

		roles = append(roles, r)
	}

	return roles, nil
}

// CreateAlert creates a new alert record.
func (rdb *RolesDatabase) CreateAlert(alertType, level, message string) error {
	_, err := rdb.db.Exec(
		"INSERT INTO alerts (type, level, message) VALUES (?, ?, ?)",
		alertType, level, message)
	return err
}

// GetUnacknowledgedAlerts returns all unacknowledged alerts.
func (rdb *RolesDatabase) GetUnacknowledgedAlerts() ([]Alert, error) {
	rows, err := rdb.db.Query(
		"SELECT id, type, level, message, created_at FROM alerts WHERE acknowledged = 0 ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.Type, &a.Level, &a.Message, &a.CreatedAt); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// AcknowledgeAlert marks an alert as acknowledged.
func (rdb *RolesDatabase) AcknowledgeAlert(alertID int) error {
	_, err := rdb.db.Exec("UPDATE alerts SET acknowledged = 1 WHERE id = ?", alertID)
	return err
}

// CleanOldAlerts removes acknowledged alerts older than the specified days.
func (rdb *RolesDatabase) CleanOldAlerts(days int) error {
	_, err := rdb.db.Exec(
		"DELETE FROM alerts WHERE acknowledged = 1 AND created_at < datetime('now', ?)",
		fmt.Sprintf("-%d days", days))
	return err
}

// Alert represents an alert record.
type Alert struct {
	ID        int       `json:"id"`
	Type      string    `json:"type"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// Close closes the database.
func (rdb *RolesDatabase) Close() error {
	return rdb.db.Close()
}
