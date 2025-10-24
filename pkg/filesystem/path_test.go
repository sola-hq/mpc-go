package filesystem

import "testing"

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid simple path", "test.json", false},
		{"valid relative path", "config/test.json", false},
		{"path traversal attempt", "../../../etc/passwd", true},
		{"path traversal with clean", "config/../../../etc/passwd", true},
		{"valid absolute path", "/tmp/test.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSafePath(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		filename string
		wantErr  bool
	}{
		{"valid file in base dir", "/tmp", "test.json", false},
		{"path traversal attempt", "/tmp", "../../../etc/passwd", true},
		{"path traversal with clean", "/tmp", "config/../../../etc/passwd", true},
		{"valid subdirectory", "/tmp", "config/test.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SafePath(tt.baseDir, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
