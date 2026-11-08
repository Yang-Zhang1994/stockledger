pipeline {
    agent any

    tools {
        go 'go1.21'
    }

    environment {
        MYSQL_DSN = 'stockledger:stockledger@tcp(127.0.0.1:3307)/stockledger?parseTime=true'
    }

    stages {
        stage('Vet') {
            steps {
                sh 'go vet ./...'
            }
        }
        stage('Unit tests') {
            steps {
                sh 'go test -race ./...'
            }
        }
        stage('Integration tests') {
            steps {
                sh '''
                    docker compose up -d --wait mysql
                    go test -tags integration ./internal/eventstore/...
                '''
            }
            post {
                always {
                    sh 'docker compose down -v'
                }
            }
        }
        stage('Image') {
            steps {
                sh 'docker build -t stockledger:${BUILD_NUMBER} .'
            }
        }
    }
}
