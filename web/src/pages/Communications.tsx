import { useState, useEffect } from 'react'

export default function Communications() {
  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">커뮤니케이션 <span className="text-gray-400 text-lg font-normal">Communications Hub</span></h1>

      <div className="grid grid-cols-3 gap-6">
        <div className="card">
          <h2 className="text-lg font-semibold mb-3">채팅 <span className="text-gray-400 text-sm font-normal">Chat</span></h2>
          <p className="text-sm text-gray-500 mb-3">하네스 내에서 안전한 엔지니어링 커뮤니케이션</p>
          <div className="space-y-1 text-sm text-gray-400">
            <div>• 직접 메시지</div>
            <div>• 그룹 채널</div>
            <div>• 세션 연결 채팅</div>
          </div>
        </div>

        <div className="card">
          <h2 className="text-lg font-semibold mb-3">파일 전송 <span className="text-gray-400 text-sm font-normal">File Transfer</span></h2>
          <p className="text-sm text-gray-500 mb-3">보안 스캔이 포함된 관리 파일 전송</p>
          <div className="space-y-1 text-sm text-gray-400">
            <div>• 바이러스 스캔</div>
            <div>• DLP 검사</div>
            <div>• 분류 태깅</div>
          </div>
        </div>

        <div className="card">
          <h2 className="text-lg font-semibold mb-3">방송 <span className="text-gray-400 text-sm font-normal">Broadcast</span></h2>
          <p className="text-sm text-gray-500 mb-3">긴급 및 관리 메시지</p>
          <div className="space-y-1 text-sm text-gray-400">
            <div>• 정보 / 경고 / 긴급</div>
            <div>• 확인 추적</div>
            <div>• 대상 지정</div>
          </div>
        </div>
      </div>

      <div className="card mt-6">
        <h2 className="text-lg font-semibold mb-3">현재 상태 <span className="text-gray-400 text-sm font-normal">Presence</span></h2>
        <p className="text-gray-400 text-sm">활성 사용자가 없습니다. PAPER 프로토콜을 통해 프레즌스가 추적됩니다.</p>
      </div>
    </div>
  )
}
