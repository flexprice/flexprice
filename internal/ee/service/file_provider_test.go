package service

import "testing"

func TestParseAllowedURL_Scheme(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"http allowed", "http://example.com/file.csv", false},
		{"https allowed", "https://example.com/file.csv", false},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"ftp scheme rejected", "ftp://example.com/file.csv", true},
		{"gopher scheme rejected", "gopher://example.com/file.csv", true},
		{"no scheme rejected", "example.com/file.csv", true},
		{"empty host rejected", "http://", true},
		{"opaque http rejected", "http:opaque", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAllowedURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAllowedURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestFileProviderRegistry_GetProvider(t *testing.T) {
	registry := NewFileProviderRegistry()

	tests := []struct {
		name string
		url  string
		want FileProviderType
	}{
		{"google drive", "https://drive.google.com/file/d/abc123/view", FileProviderTypeGoogleDrive},
		{"s3", "https://my-bucket.s3.amazonaws.com/file.csv", FileProviderTypeS3},
		{"onedrive", "https://onedrive.live.com/download?cid=1", FileProviderTypeOneDrive},
		{"dropbox", "https://www.dropbox.com/s/abc/file.csv", FileProviderTypeDropbox},
		{"github", "https://github.com/user/repo/blob/main/file.csv", FileProviderTypeGitHub},
		{"direct url", "https://example.com/file.csv", FileProviderTypeDirect},
		{"spoofed host in query is not misclassified", "https://evil.com/?x=drive.google.com", FileProviderTypeDirect},
		{"spoofed host as suffix is not misclassified", "https://drive.google.com.evil.com/file.csv", FileProviderTypeDirect},
		{"s3 with region", "https://s3.us-east-1.amazonaws.com/my-bucket/file.csv", FileProviderTypeS3},
		{"non-s3 aws service is not misclassified as s3", "https://ec2.amazonaws.com/file.csv", FileProviderTypeDirect},
		{"non-s3 domain containing s3. substring is not misclassified", "https://not-s3.example.com/file.csv", FileProviderTypeDirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.GetProvider(tt.url)
			if got.GetProviderName() != tt.want {
				t.Errorf("GetProvider(%q) = %v, want %v", tt.url, got.GetProviderName(), tt.want)
			}
		})
	}
}
